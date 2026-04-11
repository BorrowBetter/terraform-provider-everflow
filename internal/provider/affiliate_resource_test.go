// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAffiliateResource_SchemaImplementation catches framework-level schema
// bugs (e.g. an attribute accidentally marked both Required and Computed) at
// test time rather than at provider startup.
func TestAffiliateResource_SchemaImplementation(t *testing.T) {
	t.Parallel()

	r := NewAffiliateResource()
	var resp fwresource.SchemaResponse
	r.(*AffiliateResource).Schema(context.Background(), fwresource.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema returned diagnostics: %v", resp.Diagnostics)
	}

	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("Schema.ValidateImplementation: %v", diags)
	}

	// Spot-check the critical attributes exist with the expected shape so
	// refactors that accidentally rename them are caught by unit tests
	// rather than by a downstream PR's integration run.
	s, ok := resp.Schema.GetAttributes()["network_affiliate_id"]
	if !ok {
		t.Fatalf("schema missing network_affiliate_id")
	}
	if _, ok := s.(fwschema.Int64Attribute); !ok {
		t.Errorf("network_affiliate_id is %T, want schema.Int64Attribute", s)
	}
	if !s.IsComputed() {
		t.Errorf("network_affiliate_id must be Computed")
	}
	if _, ok := resp.Schema.GetAttributes()["account_status"]; !ok {
		t.Fatalf("schema missing account_status")
	}
	// Affiliate schema must NOT expose reporting_timezone_id at the top
	// level — affiliate timezones live inside nested user objects. If a
	// refactor ever copies this attribute over from the advertiser
	// resource by accident, this assertion catches it.
	if _, ok := resp.Schema.GetAttributes()["reporting_timezone_id"]; ok {
		t.Errorf("affiliate schema must not expose reporting_timezone_id as a top-level attribute")
	}
}

// TestAffiliateResource_CreateReadUpdateDelete exercises the full CRUD
// lifecycle against an httptest fake Everflow. It's the main end-to-end
// unit test for the resource and covers:
//
//   - Create sends the expected POST body and stores the returned
//     network_affiliate_id + other computed fields
//   - Read decodes a typed response
//   - Update performs fetch-modify-put: the PUT body contains both the
//     plan changes *and* a nested object the schema does not model
//   - Delete issues a PUT with account_status="inactive"
func TestAffiliateResource_CreateReadUpdateDelete(t *testing.T) {
	t.Parallel()

	srv, state := newAffiliateTestServer(t, &affiliateRecord{
		ID:                42,
		NetworkID:         1,
		Name:              "Acme Affiliate",
		AccountStatus:     "active",
		NetworkEmployeeID: 11,
		DefaultCurrencyID: "USD",
		InternalNotes:     "",
		// An unmodeled nested object the Terraform schema does not expose.
		// The fetch-modify-put Update path must preserve this field.
		Extra: map[string]any{
			"billing": map[string]any{
				"payment_type":          "wire",
				"default_payment_terms": float64(30),
			},
		},
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			// Create + Read.
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "Acme Affiliate"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_affiliate.test", "name", "Acme Affiliate"),
					resource.TestCheckResourceAttr("everflow_affiliate.test", "network_affiliate_id", "42"),
					resource.TestCheckResourceAttr("everflow_affiliate.test", "network_id", "1"),
					resource.TestCheckResourceAttr("everflow_affiliate.test", "account_status", "active"),
				),
			},
			// Update — rename + add internal_notes. The server must receive
			// a PUT that includes the unmodeled billing object.
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "Acme Renamed"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
  internal_notes      = "Managed by Terraform"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_affiliate.test", "name", "Acme Renamed"),
					resource.TestCheckResourceAttr("everflow_affiliate.test", "internal_notes", "Managed by Terraform"),
					func(_ *terraform.State) error {
						state.mu.Lock()
						defer state.mu.Unlock()
						if state.lastPutBody == nil {
							return fmt.Errorf("expected a PUT on Update, got none")
						}
						// The PUT body must contain the unmodeled
						// billing object — this is the fetch-modify-put
						// preservation contract.
						billing, ok := state.lastPutBody["billing"].(map[string]any)
						if !ok {
							return fmt.Errorf("PUT body missing preserved billing object: %v", state.lastPutBody["billing"])
						}
						if billing["payment_type"] != "wire" {
							return fmt.Errorf("PUT body billing.payment_type = %v, want wire", billing["payment_type"])
						}
						if state.lastPutBody["internal_notes"] != "Managed by Terraform" {
							return fmt.Errorf("PUT body internal_notes = %v, want 'Managed by Terraform'", state.lastPutBody["internal_notes"])
						}
						if state.lastPutBody["name"] != "Acme Renamed" {
							return fmt.Errorf("PUT body name = %v, want 'Acme Renamed'", state.lastPutBody["name"])
						}
						// After the PUT, the fake server's stored copy
						// of the record must still include the billing
						// object — this proves the preservation round-
						// tripped, not just that the resource echoed it
						// back in one direction.
						storedBilling, ok := state.record.Extra["billing"].(map[string]any)
						if !ok {
							return fmt.Errorf("stored record missing billing after Update: extras=%v", state.record.Extra)
						}
						if storedBilling["payment_type"] != "wire" {
							return fmt.Errorf("stored billing.payment_type = %v, want wire", storedBilling["payment_type"])
						}
						return nil
					},
				),
			},
		},
		// terraform destroy: verify the delete path issues a soft-delete
		// PUT rather than a DELETE. The helper's CheckDestroy runs after
		// the final destroy, by which time the server should have recorded
		// the inactive-status PUT.
		CheckDestroy: func(_ *terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.lastPutBody == nil {
				return fmt.Errorf("expected a soft-delete PUT, got none")
			}
			if state.lastPutBody["account_status"] != "inactive" {
				return fmt.Errorf("final PUT account_status = %v, want inactive", state.lastPutBody["account_status"])
			}
			if state.deleteCalled {
				return fmt.Errorf("Delete must not issue an HTTP DELETE; Everflow has no DELETE endpoint")
			}
			return nil
		},
	})
}

// TestAffiliateResource_InternalNotesClearOnUnset covers the regression
// where removing `internal_notes` from HCL previously omitted the key from
// the PUT body entirely. Because Everflow's PUT is a full replacement,
// omitting a key does *not* clear the server-side value — the resource
// must send an explicit empty string on null plans. This test fails if
// the clear-by-unset path regresses.
func TestAffiliateResource_InternalNotesClearOnUnset(t *testing.T) {
	t.Parallel()

	srv, state := newAffiliateTestServer(t, &affiliateRecord{
		ID:                42,
		Name:              "Acme Affiliate",
		AccountStatus:     "active",
		NetworkEmployeeID: 11,
		DefaultCurrencyID: "USD",
	})
	defer srv.Close()

	configWith := testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "Acme Affiliate"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
  internal_notes      = "hello"
}
`
	configWithout := testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "Acme Affiliate"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: configWith},
			{
				Config: configWithout,
				Check: func(_ *terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					v, ok := state.lastPutBody["internal_notes"]
					if !ok {
						return fmt.Errorf("PUT body omitted internal_notes; omitting would NOT clear the server value under Everflow's full-replacement PUT")
					}
					if v != "" {
						return fmt.Errorf("PUT body internal_notes = %q, want empty string to clear", v)
					}
					if state.record.InternalNotes != "" {
						return fmt.Errorf("server record internal_notes = %q after unset, want empty", state.record.InternalNotes)
					}
					return nil
				},
			},
		},
	})
}

// TestAffiliateResource_ImportByID exercises the ImportState path: a bare
// numeric ID on the CLI must be parsed to int64 and land in
// network_affiliate_id, and the subsequent Read must hydrate the rest of
// the attributes.
func TestAffiliateResource_ImportByID(t *testing.T) {
	t.Parallel()

	srv, _ := newAffiliateTestServer(t, &affiliateRecord{
		ID:                2,
		NetworkID:         1,
		Name:              "BorrowBetter",
		AccountStatus:     "active",
		NetworkEmployeeID: 11,
		DefaultCurrencyID: "USD",
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate" "imported" {
  name                = "BorrowBetter"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
}
`,
			},
			{
				ResourceName:                         "everflow_affiliate.imported",
				ImportState:                          true,
				ImportStateId:                        "2",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "network_affiliate_id",
			},
		},
	})
}

// TestAffiliateResource_ImportInvalidID verifies the string→int64 parser
// surfaces a clean diagnostic rather than silently dropping the value.
func TestAffiliateResource_ImportInvalidID(t *testing.T) {
	t.Parallel()

	srv, _ := newAffiliateTestServer(t, &affiliateRecord{
		ID:                1,
		Name:              "x",
		AccountStatus:     "active",
		NetworkEmployeeID: 1,
		DefaultCurrencyID: "USD",
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "x"
  account_status      = "active"
  network_employee_id = 1
  default_currency_id = "USD"
}
`,
			},
			{
				ResourceName:  "everflow_affiliate.test",
				ImportState:   true,
				ImportStateId: "not-a-number",
				ExpectError:   regexp.MustCompile(`network_affiliate_id must be a base-10 integer`),
			},
		},
	})
}

// TestAffiliateResource_Read404RemovesFromState simulates the "deleted
// out-of-band" case: the resource exists in Terraform state, the user runs
// refresh, and Everflow returns 404. The framework must then remove the
// resource from state (visible as the plan wanting to recreate it).
func TestAffiliateResource_Read404RemovesFromState(t *testing.T) {
	t.Parallel()

	srv, state := newAffiliateTestServer(t, &affiliateRecord{
		ID:                42,
		Name:              "Acme Affiliate",
		AccountStatus:     "active",
		NetworkEmployeeID: 11,
		DefaultCurrencyID: "USD",
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "Acme Affiliate"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
}
`,
			},
			{
				PreConfig: func() {
					// Simulate out-of-band deletion: next GET returns 404.
					state.mu.Lock()
					state.force404 = true
					state.mu.Unlock()
				},
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate" "test" {
  name                = "Acme Affiliate"
  account_status      = "active"
  network_employee_id = 11
  default_currency_id = "USD"
}
`,
				// After the refresh sees the 404, Terraform should detect
				// the resource is gone and plan a recreate — i.e. a
				// non-empty plan. ExpectNonEmptyPlan asserts exactly that,
				// which is how "removed from state" manifests at the step
				// boundary.
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

// TestAffiliateResource_InvalidAccountStatus verifies the
// stringvalidator.OneOf attached to account_status rejects values outside
// the allowed set at plan time (no HTTP call is made). For affiliates the
// allowed set is {active, inactive} only — "suspended" is explicitly NOT
// permitted, which is the primary schema-level difference from the
// advertiser resource. Both rejections are checked here so any future
// relaxation of the validator list shows up as an intentional schema
// change.
func TestAffiliateResource_InvalidAccountStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Should never be reached — plan-time validation must fire first.
		t.Errorf("HTTP request reached server despite invalid account_status")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cases := []struct {
		name  string
		value string
	}{
		{name: "bogus", value: "bogus"},
		// Affiliates cannot be "suspended" via this API, unlike
		// advertisers. If the real API ever accepts this value, relaxing
		// the validator is a backwards-compatible schema change — this
		// test makes that relaxation a visible diff rather than a silent
		// one.
		{name: "suspended", value: "suspended"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: testProviderConfig(srv) + fmt.Sprintf(`
resource "everflow_affiliate" "test" {
  name                = "x"
  account_status      = %q
  network_employee_id = 1
  default_currency_id = "USD"
}
`, tc.value),
						ExpectError: regexp.MustCompile(`(?s)account_status.*one of`),
					},
				},
			})
		})
	}
}

// affiliateRecord is the in-memory fake Everflow's view of a single
// affiliate. Extra holds any nested objects (billing, contact_address,
// users, labels, ...) the typed schema does not model so the fake can
// round-trip them on PUT exactly like the real API.
type affiliateRecord struct {
	ID                int64
	NetworkID         int64
	Name              string
	AccountStatus     string
	NetworkEmployeeID int64
	DefaultCurrencyID string
	InternalNotes     string
	Extra             map[string]any
}

// affiliateServerState is the side-channel the tests use to inspect what
// the resource actually sent over the wire. A mutex guards every field
// because terraform-plugin-testing can issue requests concurrently.
type affiliateServerState struct {
	mu           sync.Mutex
	record       *affiliateRecord
	lastPutBody  map[string]any
	deleteCalled bool
	force404     bool
}

// newAffiliateTestServer spins up a minimal in-memory fake Everflow that
// speaks the subset of the API the affiliate resource exercises:
//
//   - POST /v1/networks/affiliates — creates, returns the record with an
//     auto-assigned ID
//   - GET /v1/networks/affiliates/{id} — returns the record, honoring the
//     force404 override used by the out-of-band delete test
//   - PUT /v1/networks/affiliates/{id} — fully replaces the record from
//     the request body (Everflow's PUT semantics) and captures the raw
//     request body for the test to assert against
//   - DELETE /v1/networks/affiliates/{id} — flips deleteCalled=true so the
//     soft-delete test can assert it was NOT called
//
// The seed record is mutated in place across requests so the state survives
// across a resource.UnitTest's multi-step run.
func newAffiliateTestServer(t *testing.T, seed *affiliateRecord) (*httptest.Server, *affiliateServerState) {
	t.Helper()
	state := &affiliateServerState{record: seed}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/networks/affiliates", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Populate the server record from the POST body. Absent fields
			// stay at their seed values, which matches the real API's "PUT
			// replaces, POST creates" asymmetry closely enough for tests.
			state.record.Name = stringFromMap(body, "name")
			state.record.AccountStatus = stringFromMap(body, "account_status")
			state.record.NetworkEmployeeID = int64FromMap(body, "network_employee_id")
			state.record.DefaultCurrencyID = stringFromMap(body, "default_currency_id")
			state.record.InternalNotes = stringFromMap(body, "internal_notes")
			writeAffiliateRecord(w, state.record)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/v1/networks/affiliates/", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		// /v1/networks/affiliates/{id} — we only serve one record per
		// test server, so no id extraction is strictly necessary.
		switch r.Method {
		case http.MethodGet:
			if state.force404 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"Error":"not found"}`))
				return
			}
			writeAffiliateRecord(w, state.record)
		case http.MethodPut:
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			state.lastPutBody = body
			state.record.Name = stringFromMap(body, "name")
			state.record.AccountStatus = stringFromMap(body, "account_status")
			state.record.NetworkEmployeeID = int64FromMap(body, "network_employee_id")
			state.record.DefaultCurrencyID = stringFromMap(body, "default_currency_id")
			state.record.InternalNotes = stringFromMap(body, "internal_notes")
			// Preserve whatever the request body said about unmodeled
			// nested objects (e.g. billing, labels). The resource's
			// fetch-modify-put strategy should be echoing them back.
			state.record.Extra = map[string]any{}
			for k, v := range body {
				switch k {
				case "network_affiliate_id", "network_id", "name", "account_status",
					"network_employee_id", "default_currency_id",
					"internal_notes", "time_created", "time_saved":
					continue
				}
				state.record.Extra[k] = v
			}
			writeAffiliateRecord(w, state.record)
		case http.MethodDelete:
			state.deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(mux), state
}

// writeAffiliateRecord serializes the in-memory record back out as JSON in
// the same shape Everflow's real API uses. Extra fields are merged into the
// top-level object.
func writeAffiliateRecord(w http.ResponseWriter, rec *affiliateRecord) {
	out := map[string]any{
		"network_affiliate_id": rec.ID,
		"network_id":           rec.NetworkID,
		"name":                 rec.Name,
		"account_status":       rec.AccountStatus,
		"network_employee_id":  rec.NetworkEmployeeID,
		"default_currency_id":  rec.DefaultCurrencyID,
		"internal_notes":       rec.InternalNotes,
	}
	for k, v := range rec.Extra {
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
