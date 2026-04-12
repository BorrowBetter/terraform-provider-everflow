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

// TestOfferResource_SchemaImplementation catches framework-level schema
// bugs (e.g. an attribute accidentally marked both Required and Computed)
// at test time rather than at provider startup. It also pins the
// offer-specific structural invariant that `payout_revenue` lives in
// Schema.Blocks rather than Schema.Attributes so a future refactor that
// accidentally converts it to an attribute is caught here.
func TestOfferResource_SchemaImplementation(t *testing.T) {
	t.Parallel()

	r := NewOfferResource()
	var resp fwresource.SchemaResponse
	r.(*OfferResource).Schema(context.Background(), fwresource.SchemaRequest{}, &resp)

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
	s, ok := resp.Schema.GetAttributes()["network_offer_id"]
	if !ok {
		t.Fatalf("schema missing network_offer_id")
	}
	if _, ok := s.(fwschema.Int64Attribute); !ok {
		t.Errorf("network_offer_id is %T, want schema.Int64Attribute", s)
	}
	if !s.IsComputed() {
		t.Errorf("network_offer_id must be Computed")
	}
	if _, ok := resp.Schema.GetAttributes()["offer_status"]; !ok {
		t.Fatalf("schema missing offer_status")
	}

	// payout_revenue must live in Blocks, NOT Attributes. The fact that
	// it's a block-typed schema element is the whole reason the resource
	// has a Blocks map at all, and this assertion pins the choice.
	if _, ok := resp.Schema.GetAttributes()["payout_revenue"]; ok {
		t.Errorf("payout_revenue must NOT be an attribute — it is declared as a ListNestedBlock")
	}
	payoutBlock, ok := resp.Schema.GetBlocks()["payout_revenue"]
	if !ok {
		t.Fatalf("payout_revenue block missing from schema")
	}
	if _, ok := payoutBlock.(fwschema.ListNestedBlock); !ok {
		t.Errorf("payout_revenue is %T, want schema.ListNestedBlock", payoutBlock)
	}
}

// TestOfferResource_CreateReadUpdateDelete exercises the full CRUD
// lifecycle against an httptest fake Everflow. It's the main end-to-end
// unit test for the resource and covers:
//
//   - Create sends the expected POST body (including the payout_revenue
//     array) and stores the returned network_offer_id + other computed
//     fields
//   - Read decodes a typed response with nested payouts
//   - Update performs fetch-modify-put: the PUT body contains both the
//     plan changes (including updated payouts) *and* two unmodeled
//     nested surfaces (ruleset + traffic_filters)
//   - Delete issues a PUT with offer_status="deleted"
func TestOfferResource_CreateReadUpdateDelete(t *testing.T) {
	t.Parallel()

	srv, state := newOfferTestServer(t, &offerRecord{
		ID:                      77,
		NetworkID:               1,
		Name:                    "Acme Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
		InternalNotes:           "",
		// Two unmodeled surfaces: an object (ruleset) and an array
		// (traffic_filters). Both must survive fetch-modify-put.
		Extra: map[string]any{
			"ruleset": map[string]any{
				"countries":          []any{"US", "CA"},
				"platform_targeting": "mobile",
			},
			"traffic_filters": []any{
				map[string]any{"type": "device_type", "value": "mobile"},
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
resource "everflow_offer" "test" {
  name                       = "Acme Offer"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    entry_name     = "Base"
    payout_type    = "cpa"
    payout_amount  = 5.00
    revenue_type   = "rpa"
    revenue_amount = 10.00
    is_default     = true
    is_private     = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_offer.test", "name", "Acme Offer"),
					resource.TestCheckResourceAttr("everflow_offer.test", "network_offer_id", "77"),
					resource.TestCheckResourceAttr("everflow_offer.test", "network_id", "1"),
					resource.TestCheckResourceAttr("everflow_offer.test", "offer_status", "active"),
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.#", "1"),
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.0.payout_type", "cpa"),
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.0.revenue_type", "rpa"),
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.0.is_default", "true"),
					func(_ *terraform.State) error {
						state.mu.Lock()
						defer state.mu.Unlock()
						// The Create POST body should carry the modeled
						// payout_revenue array with the seeded CPA/RPA
						// entry — not just the scalar fields.
						if state.lastPostBody == nil {
							return fmt.Errorf("expected a POST on Create, got none")
						}
						// redirect_mode must be present in POST body —
						// this is the fix for "Field redirect_mode is
						// required".
						if state.lastPostBody["redirect_mode"] != "standard" {
							return fmt.Errorf("POST body redirect_mode = %v, want standard", state.lastPostBody["redirect_mode"])
						}
						// OfferCreateDefaults must be present in the
						// POST body — this is the fix for sequential
						// "field X is required" errors.
						if state.lastPostBody["session_definition"] != "cookie" {
							return fmt.Errorf("POST body session_definition = %v, want cookie", state.lastPostBody["session_definition"])
						}
						if state.lastPostBody["session_duration"].(float64) != 24 {
							return fmt.Errorf("POST body session_duration = %v, want 24", state.lastPostBody["session_duration"])
						}
						payouts, ok := state.lastPostBody["payout_revenue"].([]any)
						if !ok || len(payouts) != 1 {
							return fmt.Errorf("POST body payout_revenue = %v, want 1-element array", state.lastPostBody["payout_revenue"])
						}
						p0, _ := payouts[0].(map[string]any)
						if p0["payout_type"] != "cpa" {
							return fmt.Errorf("POST body payout_revenue[0].payout_type = %v, want cpa", p0["payout_type"])
						}
						if p0["is_default"] != true {
							return fmt.Errorf("POST body payout_revenue[0].is_default = %v, want true", p0["is_default"])
						}
						return nil
					},
				),
			},
			// Update — rename, add internal_notes, bump payout amount.
			// The server must receive a PUT that includes the unmodeled
			// ruleset + traffic_filters AND the updated payout array.
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "Acme Renamed"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  internal_notes = "Managed by Terraform"

  payout_revenue {
    entry_name     = "Base"
    payout_type    = "cpa"
    payout_amount  = 7.50
    revenue_type   = "rpa"
    revenue_amount = 15.00
    is_default     = true
    is_private     = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_offer.test", "name", "Acme Renamed"),
					resource.TestCheckResourceAttr("everflow_offer.test", "internal_notes", "Managed by Terraform"),
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.0.payout_amount", "7.5"),
					func(_ *terraform.State) error {
						state.mu.Lock()
						defer state.mu.Unlock()
						if state.lastPutBody == nil {
							return fmt.Errorf("expected a PUT on Update, got none")
						}
						// Both unmodeled surfaces must survive the round
						// trip — the fetch-modify-put preservation
						// contract for offers.
						ruleset, ok := state.lastPutBody["ruleset"].(map[string]any)
						if !ok {
							return fmt.Errorf("PUT body missing preserved ruleset object: %v", state.lastPutBody["ruleset"])
						}
						if ruleset["platform_targeting"] != "mobile" {
							return fmt.Errorf("PUT body ruleset.platform_targeting = %v, want mobile", ruleset["platform_targeting"])
						}
						tf, ok := state.lastPutBody["traffic_filters"].([]any)
						if !ok || len(tf) != 1 {
							return fmt.Errorf("PUT body missing preserved traffic_filters: %v", state.lastPutBody["traffic_filters"])
						}
						// The plan changes must also be present in the
						// PUT body.
						if state.lastPutBody["internal_notes"] != "Managed by Terraform" {
							return fmt.Errorf("PUT body internal_notes = %v, want 'Managed by Terraform'", state.lastPutBody["internal_notes"])
						}
						if state.lastPutBody["name"] != "Acme Renamed" {
							return fmt.Errorf("PUT body name = %v, want 'Acme Renamed'", state.lastPutBody["name"])
						}
						// The modeled payout array must be present and
						// reflect the updated values.
						payouts, ok := state.lastPutBody["payout_revenue"].([]any)
						if !ok || len(payouts) != 1 {
							return fmt.Errorf("PUT body payout_revenue = %v, want 1-element array", state.lastPutBody["payout_revenue"])
						}
						p0 := payouts[0].(map[string]any)
						if floatFromAny(p0["payout_amount"]) != 7.50 {
							return fmt.Errorf("PUT body payout_revenue[0].payout_amount = %v, want 7.5", p0["payout_amount"])
						}
						// After the PUT, the fake server's stored copy
						// of the record must still include the ruleset
						// and traffic_filters objects — this proves the
						// preservation round-tripped, not just that the
						// resource echoed them back in one direction.
						storedRuleset, ok := state.record.Extra["ruleset"].(map[string]any)
						if !ok {
							return fmt.Errorf("stored record missing ruleset after Update: extras=%v", state.record.Extra)
						}
						if storedRuleset["platform_targeting"] != "mobile" {
							return fmt.Errorf("stored ruleset.platform_targeting = %v, want mobile", storedRuleset["platform_targeting"])
						}
						if _, ok := state.record.Extra["traffic_filters"]; !ok {
							return fmt.Errorf("stored record missing traffic_filters after Update: extras=%v", state.record.Extra)
						}
						return nil
					},
				),
			},
		},
		// terraform destroy: verify the delete path issues a soft-delete
		// PUT with offer_status="deleted" rather than a DELETE. The
		// helper's CheckDestroy runs after the final destroy, by which
		// time the server should have recorded the deleted-status PUT.
		CheckDestroy: func(_ *terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.lastPutBody == nil {
				return fmt.Errorf("expected a soft-delete PUT, got none")
			}
			if state.lastPutBody["offer_status"] != "deleted" {
				return fmt.Errorf("final PUT offer_status = %v, want deleted", state.lastPutBody["offer_status"])
			}
			if state.deleteCalled {
				return fmt.Errorf("Delete must not issue an HTTP DELETE; Everflow has no DELETE endpoint")
			}
			return nil
		},
	})
}

// TestOfferResource_InternalNotesClearOnUnset covers the B1 regression
// where removing `internal_notes` from HCL previously omitted the key
// from the PUT body entirely. Because Everflow's PUT is a full
// replacement, omitting a key does *not* clear the server-side value —
// the resource must send an explicit empty string on null plans. This
// test fails if the clear-by-unset path regresses.
func TestOfferResource_InternalNotesClearOnUnset(t *testing.T) {
	t.Parallel()

	srv, state := newOfferTestServer(t, &offerRecord{
		ID:                      77,
		Name:                    "Acme Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
	})
	defer srv.Close()

	configWith := testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "Acme Offer"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5
  internal_notes             = "hello"

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`
	configWithout := testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "Acme Offer"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
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

// TestOfferResource_ImportByID exercises the ImportState path: a bare
// numeric ID on the CLI must be parsed to int64 and land in
// network_offer_id, and the subsequent Read must hydrate the rest of
// the attributes.
func TestOfferResource_ImportByID(t *testing.T) {
	t.Parallel()

	srv, _ := newOfferTestServer(t, &offerRecord{
		ID:                      2,
		NetworkID:               1,
		Name:                    "BorrowBetter Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
		PayoutRevenue: []any{
			map[string]any{
				"entry_name":     "Base",
				"payout_type":    "cpa",
				"payout_amount":  float64(5),
				"revenue_type":   "rpa",
				"revenue_amount": float64(10),
				"is_default":     true,
				"is_private":     false,
			},
		},
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "imported" {
  name                       = "BorrowBetter Offer"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    entry_name     = "Base"
    payout_type    = "cpa"
    payout_amount  = 5.00
    revenue_type   = "rpa"
    revenue_amount = 10.00
    is_default     = true
    is_private     = false
  }
}
`,
			},
			{
				ResourceName:                         "everflow_offer.imported",
				ImportState:                          true,
				ImportStateId:                        "2",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "network_offer_id",
			},
		},
	})
}

// TestOfferResource_ImportInvalidID verifies the string→int64 parser
// surfaces a clean diagnostic rather than silently dropping the value.
func TestOfferResource_ImportInvalidID(t *testing.T) {
	t.Parallel()

	srv, _ := newOfferTestServer(t, &offerRecord{
		ID:                      1,
		Name:                    "x",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "x"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`,
			},
			{
				ResourceName:  "everflow_offer.test",
				ImportState:   true,
				ImportStateId: "not-a-number",
				ExpectError:   regexp.MustCompile(`network_offer_id must be a base-10 integer`),
			},
		},
	})
}

// TestOfferResource_Read404RemovesFromState simulates the "deleted
// out-of-band" case: the resource exists in Terraform state, the user
// runs refresh, and Everflow returns 404. The framework must then remove
// the resource from state (visible as the plan wanting to recreate it).
func TestOfferResource_Read404RemovesFromState(t *testing.T) {
	t.Parallel()

	srv, state := newOfferTestServer(t, &offerRecord{
		ID:                      77,
		Name:                    "Acme Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
	})
	defer srv.Close()

	cfg := testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "Acme Offer"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					// Simulate out-of-band deletion: next GET returns 404.
					state.mu.Lock()
					state.force404 = true
					state.mu.Unlock()
				},
				Config: cfg,
				// After the refresh sees the 404, Terraform should detect
				// the resource is gone and plan a recreate — i.e. a
				// non-empty plan.
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

// TestOfferResource_InvalidOfferStatus verifies the
// stringvalidator.OneOf attached to offer_status rejects values outside
// the allowed set at plan time (no HTTP call is made). Parametrized
// over "bogus" (universally invalid) and "inactive" (valid for
// advertisers/affiliates but explicitly NOT for offers) to pin the
// schema-level difference.
func TestOfferResource_InvalidOfferStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite invalid offer_status")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cases := []struct {
		name  string
		value string
	}{
		{name: "bogus", value: "bogus"},
		// "inactive" is the advertiser/affiliate destroy status — not a
		// valid offer_status. If a future relaxation promotes it, this
		// test forces the change to be visible in the diff.
		{name: "inactive", value: "inactive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: testProviderConfig(srv) + fmt.Sprintf(`
resource "everflow_offer" "test" {
  name                       = "x"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = %q
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`, tc.value),
						ExpectError: regexp.MustCompile(`(?s)offer_status.*one of`),
					},
				},
			})
		})
	}
}

// TestOfferResource_InvalidConversionMethod verifies conversion_method's
// OneOf validator rejects non-enum values at plan time.
func TestOfferResource_InvalidConversionMethod(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite invalid conversion_method")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "x"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "bogus_method"
  network_tracking_domain_id = 5

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)conversion_method.*one of`),
			},
		},
	})
}

// TestOfferResource_InvalidPayoutType verifies the nested-attribute
// validator on payout_type rejects non-enum values at plan time.
func TestOfferResource_InvalidPayoutType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite invalid payout_type")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "x"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    payout_type  = "bogus"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)payout_type.*one of`),
			},
		},
	})
}

// TestOfferResource_NestedPayoutRevenueRoundTrip covers the real-world
// Everflow shape where GET responses nest `payout_revenue` under
// `relationship.payout_revenue.entries` instead of returning it at the
// top level. A plain decoder that only looks at the top-level key
// silently drops the payouts on Read, which breaks `terraform import`
// against the real API. This test locks in:
//
//   - Import hydrates the model from the nested GET shape correctly
//     (the custom UnmarshalJSON on Offer)
//   - Update strips `relationship.payout_revenue` before writing the
//     top-level overlay, so the PUT body carries only one
//     authoritative payout_revenue shape
//   - Sibling keys under `relationship` (e.g. labels) still survive
//     fetch-modify-put untouched
//   - `payout_type = "null_value"` is accepted by the schema, because
//     ~75% of real offers have a secondary entry with that value
func TestOfferResource_NestedPayoutRevenueRoundTrip(t *testing.T) {
	t.Parallel()

	srv, state := newOfferTestServer(t, &offerRecord{
		ID:                      77,
		NetworkID:               1,
		Name:                    "Nested Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
		PayoutRevenue: []any{
			map[string]any{
				"entry_name":         "Base",
				"payout_type":        "cpa",
				"payout_percentage":  float64(90),
				"revenue_type":       "rps",
				"revenue_percentage": float64(100),
				"is_default":         true,
				"is_private":         false,
			},
			map[string]any{
				"entry_name":         "Revenue Received",
				"payout_type":        "null_value",
				"revenue_type":       "rps",
				"revenue_percentage": float64(100),
				"is_default":         false,
				"is_private":         true,
			},
		},
		// Sibling under `relationship` that must survive unchanged.
		Extra: map[string]any{
			"relationship": map[string]any{
				"labels": []any{"featured"},
			},
		},
	})
	state.nestPayoutRevenue = true
	defer srv.Close()

	cfg := testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "Nested Offer"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    entry_name         = "Base"
    payout_type        = "cpa"
    payout_percentage  = 90
    revenue_type       = "rps"
    revenue_percentage = 100
    is_default         = true
    is_private         = false
  }

  payout_revenue {
    entry_name         = "Revenue Received"
    payout_type        = "null_value"
    revenue_type       = "rps"
    revenue_percentage = 100
    is_default         = false
    is_private         = true
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Create exercises the custom UnmarshalJSON via the
				// POST response (the fake server returns the nested
				// shape when state.nestPayoutRevenue is set), plus
				// the framework's post-Create refresh via GET.
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.#", "2"),
					resource.TestCheckResourceAttr("everflow_offer.test", "payout_revenue.1.payout_type", "null_value"),
				),
			},
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "Nested Offer Renamed"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5

  payout_revenue {
    entry_name         = "Base"
    payout_type        = "cpa"
    payout_percentage  = 90
    revenue_type       = "rps"
    revenue_percentage = 100
    is_default         = true
    is_private         = false
  }

  payout_revenue {
    entry_name         = "Revenue Received"
    payout_type        = "null_value"
    revenue_type       = "rps"
    revenue_percentage = 100
    is_default         = false
    is_private         = true
  }
}
`,
				Check: func(_ *terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					if state.lastPutBody == nil {
						return fmt.Errorf("expected a PUT on Update, got none")
					}
					// Top-level payout_revenue must be present and
					// carry both entries, including the null_value one.
					payouts, ok := state.lastPutBody["payout_revenue"].([]any)
					if !ok || len(payouts) != 2 {
						return fmt.Errorf("PUT body payout_revenue = %v, want 2-element array", state.lastPutBody["payout_revenue"])
					}
					p1, _ := payouts[1].(map[string]any)
					if p1["payout_type"] != "null_value" {
						return fmt.Errorf("PUT body payout_revenue[1].payout_type = %v, want null_value", p1["payout_type"])
					}
					// Relationship wrapper must survive, but its
					// payout_revenue child must be stripped so the PUT
					// carries only the top-level shape.
					rel, ok := state.lastPutBody["relationship"].(map[string]any)
					if !ok {
						return fmt.Errorf("PUT body missing preserved relationship object: %v", state.lastPutBody["relationship"])
					}
					if _, bad := rel["payout_revenue"]; bad {
						return fmt.Errorf("PUT body relationship.payout_revenue must be stripped, got %v", rel["payout_revenue"])
					}
					// Sibling labels under relationship must survive.
					labels, ok := rel["labels"].([]any)
					if !ok || len(labels) != 1 || labels[0] != "featured" {
						return fmt.Errorf("PUT body relationship.labels not preserved: %v", rel["labels"])
					}
					return nil
				},
			},
		},
	})
}

// TestOfferResource_RequiresPayoutRevenueBlock verifies that omitting
// the payout_revenue block entirely fails plan-time validation via
// listvalidator.IsRequired() — SizeAtLeast alone skips null values, so
// both validators are attached to the block to cover omitted and empty
// configurations.
func TestOfferResource_RequiresPayoutRevenueBlock(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite missing payout_revenue")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "x"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5
}
`,
				ExpectError: regexp.MustCompile(`(?s)payout_revenue.*required`),
			},
		},
	})
}

// TestOfferResource_InvalidVisibility verifies the visibility validator
// rejects values outside the allowed set at plan time.
func TestOfferResource_InvalidVisibility(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite invalid visibility")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_offer" "test" {
  name                       = "x"
  network_advertiser_id      = 42
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 5
  visibility                 = "bogus"

  payout_revenue {
    payout_type  = "cpa"
    revenue_type = "rpa"
    is_default   = true
    is_private   = false
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)visibility.*one of`),
			},
		},
	})
}

// offerRecord is the in-memory fake Everflow's view of a single offer.
// Extra holds any nested objects (ruleset, traffic_filters, creatives,
// labels, ...) the typed schema does not model so the fake can round-
// trip them on PUT exactly like the real API. PayoutRevenue is modeled
// explicitly because the resource manages it as schema state.
type offerRecord struct {
	ID                      int64
	NetworkID               int64
	Name                    string
	NetworkAdvertiserID     int64
	DestinationURL          string
	OfferStatus             string
	RedirectMode            string
	Visibility              string
	CurrencyID              string
	ConversionMethod        string
	NetworkTrackingDomainID int64
	InternalNotes           string
	PayoutRevenue           []any
	Extra                   map[string]any
}

// offerServerState is the side-channel the tests use to inspect what
// the resource actually sent over the wire. A mutex guards every field
// because terraform-plugin-testing can issue requests concurrently.
type offerServerState struct {
	mu           sync.Mutex
	record       *offerRecord
	lastPostBody map[string]any
	lastPutBody  map[string]any
	deleteCalled bool
	force404     bool
	// nestPayoutRevenue makes the fake server return payout_revenue
	// nested under `relationship.payout_revenue.entries` on GET,
	// matching the real Everflow API shape. Used by the regression
	// test that covers the custom UnmarshalJSON path and the
	// strip-on-PUT overlay behavior.
	nestPayoutRevenue bool
}

// newOfferTestServer spins up a minimal in-memory fake Everflow that
// speaks the subset of the API the offer resource exercises:
//
//   - POST /v1/networks/offers — creates, returns the record with the
//     seeded ID
//   - GET /v1/networks/offers/{id} — returns the record, honoring the
//     force404 override used by the out-of-band delete test
//   - PUT /v1/networks/offers/{id} — fully replaces the record from the
//     request body (Everflow's PUT semantics) and captures the raw
//     request body for the test to assert against
//   - DELETE /v1/networks/offers/{id} — flips deleteCalled=true so the
//     soft-delete test can assert it was NOT called
//
// The seed record is mutated in place across requests so the state
// survives across a resource.UnitTest's multi-step run.
func newOfferTestServer(t *testing.T, seed *offerRecord) (*httptest.Server, *offerServerState) {
	t.Helper()
	state := &offerServerState{record: seed}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/networks/offers", func(w http.ResponseWriter, r *http.Request) {
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
			state.lastPostBody = body
			// Populate the server record from the POST body. Absent
			// fields stay at their seed values, which matches the real
			// API's "PUT replaces, POST creates" asymmetry closely enough
			// for tests.
			state.record.Name = stringFromMap(body, "name")
			state.record.NetworkAdvertiserID = int64FromMap(body, "network_advertiser_id")
			state.record.DestinationURL = stringFromMap(body, "destination_url")
			state.record.OfferStatus = stringFromMap(body, "offer_status")
			state.record.RedirectMode = stringFromMap(body, "redirect_mode")
			state.record.Visibility = stringFromMap(body, "visibility")
			state.record.CurrencyID = stringFromMap(body, "currency_id")
			state.record.ConversionMethod = stringFromMap(body, "conversion_method")
			state.record.NetworkTrackingDomainID = int64FromMap(body, "network_tracking_domain_id")
			state.record.InternalNotes = stringFromMap(body, "internal_notes")
			if pr, ok := body["payout_revenue"].([]any); ok {
				state.record.PayoutRevenue = pr
			}
			writeOfferRecord(w, state.record, state.nestPayoutRevenue)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/v1/networks/offers/", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		// /v1/networks/offers/{id} — we only serve one record per test
		// server, so no id extraction is strictly necessary.
		switch r.Method {
		case http.MethodGet:
			if state.force404 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"Error":"not found"}`))
				return
			}
			writeOfferRecord(w, state.record, state.nestPayoutRevenue)
		case http.MethodPut:
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			state.lastPutBody = body
			state.record.Name = stringFromMap(body, "name")
			state.record.NetworkAdvertiserID = int64FromMap(body, "network_advertiser_id")
			state.record.DestinationURL = stringFromMap(body, "destination_url")
			state.record.OfferStatus = stringFromMap(body, "offer_status")
			state.record.RedirectMode = stringFromMap(body, "redirect_mode")
			state.record.Visibility = stringFromMap(body, "visibility")
			state.record.CurrencyID = stringFromMap(body, "currency_id")
			state.record.ConversionMethod = stringFromMap(body, "conversion_method")
			state.record.NetworkTrackingDomainID = int64FromMap(body, "network_tracking_domain_id")
			state.record.InternalNotes = stringFromMap(body, "internal_notes")
			if pr, ok := body["payout_revenue"].([]any); ok {
				state.record.PayoutRevenue = pr
			}
			// Preserve whatever the request body said about unmodeled
			// nested objects (e.g. ruleset, traffic_filters, labels). The
			// resource's fetch-modify-put strategy should be echoing them
			// back.
			state.record.Extra = map[string]any{}
			for k, v := range body {
				switch k {
				case "network_offer_id", "network_id", "name",
					"network_advertiser_id", "destination_url",
					"offer_status", "redirect_mode", "visibility",
					"currency_id", "conversion_method",
					"network_tracking_domain_id",
					"internal_notes", "payout_revenue",
					"time_created", "time_saved":
					continue
				}
				state.record.Extra[k] = v
			}
			writeOfferRecord(w, state.record, state.nestPayoutRevenue)
		case http.MethodDelete:
			state.deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(mux), state
}

// writeOfferRecord serializes the in-memory record back out as JSON in
// the same shape Everflow's real API uses. Extra fields are merged into
// the top-level object. When nested is true, payout_revenue is wrapped
// inside `relationship.payout_revenue.entries` instead of being placed
// at the top level — matching the real Everflow GET response shape.
func writeOfferRecord(w http.ResponseWriter, rec *offerRecord, nested bool) {
	vis := rec.Visibility
	if vis == "" {
		vis = "public" // Server default.
	}
	rm := rec.RedirectMode
	if rm == "" {
		rm = "standard" // Server default.
	}
	out := map[string]any{
		"network_offer_id":           rec.ID,
		"network_id":                 rec.NetworkID,
		"name":                       rec.Name,
		"network_advertiser_id":      rec.NetworkAdvertiserID,
		"destination_url":            rec.DestinationURL,
		"offer_status":               rec.OfferStatus,
		"redirect_mode":              rm,
		"visibility":                 vis,
		"currency_id":                rec.CurrencyID,
		"conversion_method":          rec.ConversionMethod,
		"network_tracking_domain_id": rec.NetworkTrackingDomainID,
		"internal_notes":             rec.InternalNotes,
	}
	if nested {
		// Merge with any pre-existing relationship from Extra so
		// other unmodeled keys (labels, category, ...) still survive.
		rel := map[string]any{}
		if existing, ok := rec.Extra["relationship"].(map[string]any); ok {
			for k, v := range existing {
				rel[k] = v
			}
		}
		if rec.PayoutRevenue != nil {
			rel["payout_revenue"] = map[string]any{
				"total":   len(rec.PayoutRevenue),
				"entries": rec.PayoutRevenue,
			}
		}
		out["relationship"] = rel
	} else if rec.PayoutRevenue != nil {
		out["payout_revenue"] = rec.PayoutRevenue
	}
	for k, v := range rec.Extra {
		if nested && k == "relationship" {
			// Already merged into out["relationship"] above.
			continue
		}
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// floatFromAny normalizes a JSON-decoded numeric value to float64. JSON
// numbers always decode into float64 via encoding/json into a
// map[string]any, but we go through the type assertion explicitly to
// keep the test-time diagnostic message readable on the failure path.
func floatFromAny(v any) float64 {
	f, _ := v.(float64)
	return f
}
