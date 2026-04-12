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

func TestAffiliateOfferVisibilityResource_SchemaImplementation(t *testing.T) {
	t.Parallel()

	r := NewAffiliateOfferVisibilityResource()
	var resp fwresource.SchemaResponse
	r.(*AffiliateOfferVisibilityResource).Schema(context.Background(), fwresource.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema returned diagnostics: %v", resp.Diagnostics)
	}

	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("Schema.ValidateImplementation: %v", diags)
	}

	s, ok := resp.Schema.GetAttributes()["network_affiliate_id"]
	if !ok {
		t.Fatalf("schema missing network_affiliate_id")
	}
	if _, ok := s.(fwschema.Int64Attribute); !ok {
		t.Errorf("network_affiliate_id is %T, want schema.Int64Attribute", s)
	}
	if !s.IsRequired() {
		t.Errorf("network_affiliate_id must be Required")
	}

	s, ok = resp.Schema.GetAttributes()["network_offer_id"]
	if !ok {
		t.Fatalf("schema missing network_offer_id")
	}
	if _, ok := s.(fwschema.Int64Attribute); !ok {
		t.Errorf("network_offer_id is %T, want schema.Int64Attribute", s)
	}
	if !s.IsRequired() {
		t.Errorf("network_offer_id must be Required")
	}
}

// TestAffiliateOfferVisibilityResource_CreateReadDelete exercises the
// full lifecycle: Create PATCHes visible, Read confirms the affiliate
// is in the visible list, Delete PATCHes hidden.
func TestAffiliateOfferVisibilityResource_CreateReadDelete(t *testing.T) {
	t.Parallel()

	srv, state := newVisibilityTestServer(t)
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_offer_visibility" "test" {
  network_affiliate_id = 7
  network_offer_id     = 67
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("everflow_affiliate_offer_visibility.test", "network_affiliate_id", "7"),
					resource.TestCheckResourceAttr("everflow_affiliate_offer_visibility.test", "network_offer_id", "67"),
					func(_ *terraform.State) error {
						state.mu.Lock()
						defer state.mu.Unlock()
						if state.lastPatchBody == nil {
							return fmt.Errorf("expected a PATCH on Create, got none")
						}
						if state.lastPatchBody["visibility_type"] != "visible" {
							return fmt.Errorf("PATCH visibility_type = %v, want visible", state.lastPatchBody["visibility_type"])
						}
						return nil
					},
				),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.lastPatchBody == nil {
				return fmt.Errorf("expected a PATCH on Delete, got none")
			}
			if state.lastPatchBody["visibility_type"] != "hidden" {
				return fmt.Errorf("Delete PATCH visibility_type = %v, want hidden", state.lastPatchBody["visibility_type"])
			}
			return nil
		},
	})
}

// TestAffiliateOfferVisibilityResource_Read404RemovesFromState simulates
// the offer being deleted out-of-band: GET /offers/{id}/visibility returns
// 404, and the resource is removed from state.
func TestAffiliateOfferVisibilityResource_Read404RemovesFromState(t *testing.T) {
	t.Parallel()

	srv, state := newVisibilityTestServer(t)
	defer srv.Close()

	cfg := testProviderConfig(srv) + `
resource "everflow_affiliate_offer_visibility" "test" {
  network_affiliate_id = 7
  network_offer_id     = 67
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

// TestAffiliateOfferVisibilityResource_ReadHiddenRemovesFromState
// simulates the affiliate being hidden out-of-band: the GET still
// succeeds but the affiliate is no longer in the visible list.
func TestAffiliateOfferVisibilityResource_ReadHiddenRemovesFromState(t *testing.T) {
	t.Parallel()

	srv, state := newVisibilityTestServer(t)
	defer srv.Close()

	cfg := testProviderConfig(srv) + `
resource "everflow_affiliate_offer_visibility" "test" {
  network_affiliate_id = 7
  network_offer_id     = 67
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					state.mu.Lock()
					state.forceHidden = true
					state.mu.Unlock()
				},
				Config:             cfg,
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

func TestAffiliateOfferVisibilityResource_ImportByCompositeID(t *testing.T) {
	t.Parallel()

	srv, _ := newVisibilityTestServer(t)
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig(srv) + `
resource "everflow_affiliate_offer_visibility" "imported" {
  network_affiliate_id = 7
  network_offer_id     = 67
}
`,
			},
			{
				ResourceName:                         "everflow_affiliate_offer_visibility.imported",
				ImportState:                          true,
				ImportStateId:                        "7/67",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "network_affiliate_id",
			},
		},
	})
}

func TestAffiliateOfferVisibilityResource_ImportInvalidID(t *testing.T) {
	t.Parallel()

	srv, _ := newVisibilityTestServer(t)
	defer srv.Close()

	cases := []struct {
		name string
		id   string
		want string
	}{
		{name: "no_slash", id: "abc", want: `expected format: affiliate_id/offer_id`},
		{name: "bad_affiliate", id: "abc/67", want: `affiliate_id must be a base-10 integer`},
		{name: "bad_offer", id: "7/xyz", want: `offer_id must be a base-10 integer`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: testProviderConfig(srv) + `
resource "everflow_affiliate_offer_visibility" "test" {
  network_affiliate_id = 7
  network_offer_id     = 67
}
`,
					},
					{
						ResourceName:  "everflow_affiliate_offer_visibility.test",
						ImportState:   true,
						ImportStateId: tc.id,
						ExpectError:   regexp.MustCompile(tc.want),
					},
				},
			})
		})
	}
}

// visibilityServerState tracks what the fake server received.
type visibilityServerState struct {
	mu            sync.Mutex
	visibleIDs    []int64
	lastPatchBody map[string]any
	force404      bool
	forceHidden   bool
}

// newVisibilityTestServer spins up a fake Everflow that handles:
//   - PATCH /v1/networks/affiliates/{id}/offers/visibility
//   - GET /v1/networks/offers/{id}/visibility
func newVisibilityTestServer(t *testing.T) (*httptest.Server, *visibilityServerState) {
	t.Helper()
	state := &visibilityServerState{
		visibleIDs: []int64{},
	}

	mux := http.NewServeMux()

	// PATCH /v1/networks/affiliates/{id}/offers/visibility
	mux.HandleFunc("/v1/networks/affiliates/", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state.lastPatchBody = body

		visType := stringFromMap(body, "visibility_type")
		offerIDs, _ := body["network_offer_ids"].([]any)

		for _, raw := range offerIDs {
			id := int64(raw.(float64))
			if visType == "visible" {
				// Add to visible list (idempotent).
				found := false
				for _, v := range state.visibleIDs {
					if v == id {
						found = true
						break
					}
				}
				if !found {
					state.visibleIDs = append(state.visibleIDs, id)
				}
			} else if visType == "hidden" {
				// Remove from visible list.
				filtered := state.visibleIDs[:0]
				for _, v := range state.visibleIDs {
					if v != id {
						filtered = append(filtered, v)
					}
				}
				state.visibleIDs = filtered
			}
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// GET /v1/networks/offers/{id}/visibility
	mux.HandleFunc("/v1/networks/offers/", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if state.force404 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"Error":"not found"}`))
			return
		}

		// Build response: affiliate 7 is visible unless forceHidden.
		visIDs := []int64{}
		if !state.forceHidden {
			visIDs = append(visIDs, 7) // The affiliate used in tests.
		}

		out := map[string]any{
			"network_id":                     1,
			"network_offer_id":               67,
			"network_affiliate_visible_ids":  visIDs,
			"network_affiliate_rejected_ids": []int64{},
			"network_affiliate_hidden_ids":   []int64{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	return httptest.NewServer(mux), state
}
