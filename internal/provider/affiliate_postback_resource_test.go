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

func TestAffiliatePostbackResource_SchemaImplementation(t *testing.T) {
	t.Parallel()

	r := NewAffiliatePostbackResource()
	var resp fwresource.SchemaResponse
	r.(*AffiliatePostbackResource).Schema(context.Background(), fwresource.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema returned diagnostics: %v", resp.Diagnostics)
	}

	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("Schema.ValidateImplementation: %v", diags)
	}

	s, ok := resp.Schema.GetAttributes()["network_pixel_id"]
	if !ok {
		t.Fatalf("schema missing network_pixel_id")
	}
	if _, ok := s.(fwschema.Int64Attribute); !ok {
		t.Errorf("network_pixel_id is %T, want schema.Int64Attribute", s)
	}
	if !s.IsComputed() {
		t.Errorf("network_pixel_id must be Computed")
	}

	if _, ok := resp.Schema.GetAttributes()["postback_url"]; !ok {
		t.Fatalf("schema missing postback_url")
	}
	if _, ok := resp.Schema.GetAttributes()["pixel_status"]; !ok {
		t.Fatalf("schema missing pixel_status")
	}
}

// TestAffiliatePostbackResource_CreateReadUpdateDelete exercises the
// full CRUD lifecycle against an httptest fake. Covers:
//   - Create sends the expected POST body with hardcoded delivery_method
//     and pixel_level
//   - Read decodes the typed response
//   - Update performs fetch-modify-put preserving unmodeled fields
//   - Delete issues a soft-delete PUT with pixel_status="inactive"
func TestAffiliatePostbackResource_CreateReadUpdateDelete(t *testing.T) {
	t.Parallel()

	srv, state := newPostbackTestServer(t, &postbackRecord{
		ID:                 3,
		NetworkID:          1,
		NetworkAffiliateID: 7,
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback",
		Description:        "",
		Extra: map[string]any{
			"facebook_pixel": map[string]any{"pixel_id": "12345"},
		},
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			// Create + Read.
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "active"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "network_pixel_id", "3"),
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "network_id", "1"),
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "network_affiliate_id", "7"),
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "pixel_type", "conversion"),
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "pixel_status", "active"),
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "postback_url", "https://example.com/postback"),
					func(_ *terraform.State) error {
						state.mu.Lock()
						defer state.mu.Unlock()
						if state.lastPostBody == nil {
							return fmt.Errorf("expected a POST on Create, got none")
						}
						if state.lastPostBody["delivery_method"] != "postback" {
							return fmt.Errorf("POST delivery_method = %v, want postback", state.lastPostBody["delivery_method"])
						}
						if state.lastPostBody["pixel_level"] != "global" {
							return fmt.Errorf("POST pixel_level = %v, want global", state.lastPostBody["pixel_level"])
						}
						return nil
					},
				),
			},
			// Update — change URL and add description.
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/updated"
  pixel_type           = "conversion"
  pixel_status         = "active"
  description          = "Updated postback"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "postback_url", "https://example.com/updated"),
					resource.TestCheckResourceAttr("everflow_affiliate_postback.test", "description", "Updated postback"),
					func(_ *terraform.State) error {
						state.mu.Lock()
						defer state.mu.Unlock()
						if state.lastPutBody == nil {
							return fmt.Errorf("expected a PUT on Update, got none")
						}
						// Unmodeled field must survive fetch-modify-put.
						fb, ok := state.lastPutBody["facebook_pixel"].(map[string]any)
						if !ok {
							return fmt.Errorf("PUT body missing preserved facebook_pixel: %v", state.lastPutBody["facebook_pixel"])
						}
						if fb["pixel_id"] != "12345" {
							return fmt.Errorf("PUT body facebook_pixel.pixel_id = %v, want 12345", fb["pixel_id"])
						}
						if state.lastPutBody["postback_url"] != "https://example.com/updated" {
							return fmt.Errorf("PUT body postback_url = %v, want updated URL", state.lastPutBody["postback_url"])
						}
						if state.lastPutBody["description"] != "Updated postback" {
							return fmt.Errorf("PUT body description = %v, want 'Updated postback'", state.lastPutBody["description"])
						}
						// Hardcoded fields must be present.
						if state.lastPutBody["delivery_method"] != "postback" {
							return fmt.Errorf("PUT delivery_method = %v, want postback", state.lastPutBody["delivery_method"])
						}
						if state.lastPutBody["pixel_level"] != "global" {
							return fmt.Errorf("PUT pixel_level = %v, want global", state.lastPutBody["pixel_level"])
						}
						return nil
					},
				),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.lastPutBody == nil {
				return fmt.Errorf("expected a soft-delete PUT, got none")
			}
			if state.lastPutBody["pixel_status"] != "inactive" {
				return fmt.Errorf("final PUT pixel_status = %v, want inactive", state.lastPutBody["pixel_status"])
			}
			if state.deleteCalled {
				return fmt.Errorf("Delete must not issue an HTTP DELETE")
			}
			return nil
		},
	})
}

// TestAffiliatePostbackResource_DescriptionClearOnUnset is the B1
// regression guard for the description field, analogous to
// InternalNotesClearOnUnset on offer/affiliate.
func TestAffiliatePostbackResource_DescriptionClearOnUnset(t *testing.T) {
	t.Parallel()

	srv, state := newPostbackTestServer(t, &postbackRecord{
		ID:                 3,
		NetworkID:          1,
		NetworkAffiliateID: 7,
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback",
	})
	defer srv.Close()

	configWith := testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "active"
  description          = "hello"
}
`
	configWithout := testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "active"
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
					v, ok := state.lastPutBody["description"]
					if !ok {
						return fmt.Errorf("PUT body omitted description; omitting would NOT clear the server value under Everflow's full-replacement PUT")
					}
					if v != "" {
						return fmt.Errorf("PUT body description = %q, want empty string to clear", v)
					}
					if state.record.Description != "" {
						return fmt.Errorf("server record description = %q after unset, want empty", state.record.Description)
					}
					return nil
				},
			},
		},
	})
}

func TestAffiliatePostbackResource_ImportByID(t *testing.T) {
	t.Parallel()

	srv, _ := newPostbackTestServer(t, &postbackRecord{
		ID:                 3,
		NetworkID:          1,
		NetworkAffiliateID: 7,
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback",
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "imported" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "active"
}
`,
			},
			{
				ResourceName:                         "everflow_affiliate_postback.imported",
				ImportState:                          true,
				ImportStateId:                        "3",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "network_pixel_id",
			},
		},
	})
}

func TestAffiliatePostbackResource_ImportInvalidID(t *testing.T) {
	t.Parallel()

	srv, _ := newPostbackTestServer(t, &postbackRecord{
		ID:                 3,
		NetworkID:          1,
		NetworkAffiliateID: 7,
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback",
	})
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "active"
}
`,
			},
			{
				ResourceName:  "everflow_affiliate_postback.test",
				ImportState:   true,
				ImportStateId: "not-a-number",
				ExpectError:   regexp.MustCompile(`network_pixel_id must be a base-10 integer`),
			},
		},
	})
}

func TestAffiliatePostbackResource_Read404RemovesFromState(t *testing.T) {
	t.Parallel()

	srv, state := newPostbackTestServer(t, &postbackRecord{
		ID:                 3,
		NetworkID:          1,
		NetworkAffiliateID: 7,
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback",
	})
	defer srv.Close()

	cfg := testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "active"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					state.mu.Lock()
					state.force404 = true
					state.mu.Unlock()
				},
				Config:             cfg,
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

func TestAffiliatePostbackResource_InvalidPixelType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite invalid pixel_type")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "bogus"
  pixel_status         = "active"
}
`,
				ExpectError: regexp.MustCompile(`(?s)pixel_type.*one of`),
			},
		},
	})
}

func TestAffiliatePostbackResource_InvalidPixelStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached server despite invalid pixel_status")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_postback" "test" {
  network_affiliate_id = 7
  postback_url         = "https://example.com/postback"
  pixel_type           = "conversion"
  pixel_status         = "bogus"
}
`,
				ExpectError: regexp.MustCompile(`(?s)pixel_status.*one of`),
			},
		},
	})
}

// postbackRecord is the in-memory fake Everflow's view of a single pixel.
type postbackRecord struct {
	ID                 int64
	NetworkID          int64
	NetworkAffiliateID int64
	PixelType          string
	PixelStatus        string
	PostbackURL        string
	DelayMS            int64
	Description        string
	Extra              map[string]any
}

type postbackServerState struct {
	mu           sync.Mutex
	record       *postbackRecord
	lastPostBody map[string]any
	lastPutBody  map[string]any
	deleteCalled bool
	force404     bool
}

func newPostbackTestServer(t *testing.T, seed *postbackRecord) (*httptest.Server, *postbackServerState) {
	t.Helper()
	state := &postbackServerState{record: seed}

	mux := http.NewServeMux()

	// POST /v1/networks/pixels
	mux.HandleFunc("/v1/networks/pixels", func(w http.ResponseWriter, r *http.Request) {
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
			state.record.NetworkAffiliateID = int64FromMap(body, "network_affiliate_id")
			state.record.PixelType = stringFromMap(body, "pixel_type")
			state.record.PixelStatus = stringFromMap(body, "pixel_status")
			state.record.PostbackURL = stringFromMap(body, "postback_url")
			state.record.DelayMS = int64FromMap(body, "delay_ms")
			state.record.Description = stringFromMap(body, "description")
			writePostbackRecord(w, state.record)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// GET/PUT/DELETE /v1/networks/pixels/{id}
	mux.HandleFunc("/v1/networks/pixels/", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			if state.force404 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"Error":"not found"}`))
				return
			}
			writePostbackRecord(w, state.record)
		case http.MethodPut:
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			state.lastPutBody = body
			state.record.NetworkAffiliateID = int64FromMap(body, "network_affiliate_id")
			state.record.PixelType = stringFromMap(body, "pixel_type")
			state.record.PixelStatus = stringFromMap(body, "pixel_status")
			state.record.PostbackURL = stringFromMap(body, "postback_url")
			state.record.DelayMS = int64FromMap(body, "delay_ms")
			state.record.Description = stringFromMap(body, "description")
			// Preserve unmodeled fields from the PUT body.
			state.record.Extra = map[string]any{}
			for k, v := range body {
				switch k {
				case "network_pixel_id", "network_id",
					"network_affiliate_id", "delivery_method",
					"pixel_level", "pixel_type", "pixel_status",
					"postback_url", "delay_ms", "description",
					"time_created", "time_saved":
					continue
				}
				state.record.Extra[k] = v
			}
			writePostbackRecord(w, state.record)
		case http.MethodDelete:
			state.deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(mux), state
}

func writePostbackRecord(w http.ResponseWriter, rec *postbackRecord) {
	out := map[string]any{
		"network_pixel_id":     rec.ID,
		"network_id":           rec.NetworkID,
		"network_affiliate_id": rec.NetworkAffiliateID,
		"delivery_method":      "postback",
		"pixel_level":          "global",
		"pixel_type":           rec.PixelType,
		"pixel_status":         rec.PixelStatus,
		"postback_url":         rec.PostbackURL,
		"delay_ms":             rec.DelayMS,
		"description":          rec.Description,
		"time_created":         1700000000,
		"time_saved":           1700000001,
	}
	for k, v := range rec.Extra {
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
