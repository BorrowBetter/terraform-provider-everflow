// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetOfferVisibility_DecodesResponse(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_id": 1,
			"network_offer_id": 67,
			"network_affiliate_visible_ids": [7, 12],
			"network_affiliate_rejected_ids": [3],
			"network_affiliate_hidden_ids": [8, 29]
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetOfferVisibility(context.Background(), 67)
	if err != nil {
		t.Fatalf("GetOfferVisibility returned error: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/networks/offers/67/visibility" {
		t.Errorf("path = %q, want /v1/networks/offers/67/visibility", gotPath)
	}
	if got.NetworkOfferID != 67 {
		t.Errorf("NetworkOfferID = %d, want 67", got.NetworkOfferID)
	}
	if len(got.NetworkAffiliateVisibleIDs) != 2 || got.NetworkAffiliateVisibleIDs[0] != 7 {
		t.Errorf("NetworkAffiliateVisibleIDs = %v, want [7, 12]", got.NetworkAffiliateVisibleIDs)
	}
	if len(got.NetworkAffiliateRejectedIDs) != 1 || got.NetworkAffiliateRejectedIDs[0] != 3 {
		t.Errorf("NetworkAffiliateRejectedIDs = %v, want [3]", got.NetworkAffiliateRejectedIDs)
	}
	if len(got.NetworkAffiliateHiddenIDs) != 2 || got.NetworkAffiliateHiddenIDs[0] != 8 {
		t.Errorf("NetworkAffiliateHiddenIDs = %v, want [8, 29]", got.NetworkAffiliateHiddenIDs)
	}
}

func TestSetAffiliateOfferVisibility_PatchesExpectedBody(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		// Return 200 with empty body rather than 204 so the HTTP
		// client fully drains the connection before srv.Close().
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	err := c.SetAffiliateOfferVisibility(context.Background(), 7, []int64{67, 68}, "visible")
	if err != nil {
		t.Fatalf("SetAffiliateOfferVisibility returned error: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/v1/networks/affiliates/7/offers/visibility" {
		t.Errorf("path = %q, want /v1/networks/affiliates/7/offers/visibility", gotPath)
	}
	if gotBody["visibility_type"] != "visible" {
		t.Errorf("body.visibility_type = %v, want visible", gotBody["visibility_type"])
	}
	offerIDs, ok := gotBody["network_offer_ids"].([]any)
	if !ok || len(offerIDs) != 2 {
		t.Fatalf("body.network_offer_ids = %v, want 2-element array", gotBody["network_offer_ids"])
	}
	if offerIDs[0].(float64) != 67 || offerIDs[1].(float64) != 68 {
		t.Errorf("body.network_offer_ids = %v, want [67, 68]", offerIDs)
	}
}

func TestSetAffiliateOfferVisibility_Hidden(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	err := c.SetAffiliateOfferVisibility(context.Background(), 7, []int64{67}, "hidden")
	if err != nil {
		t.Fatalf("SetAffiliateOfferVisibility returned error: %v", err)
	}

	if gotBody["visibility_type"] != "hidden" {
		t.Errorf("body.visibility_type = %v, want hidden", gotBody["visibility_type"])
	}
}

func TestGetOfferVisibility_NotFoundBubblesUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"Error":"not found"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.GetOfferVisibility(context.Background(), 99)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, want true (err=%v)", err)
	}
}

func TestSetAffiliateOfferVisibility_BadRequestDecodesAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"invalid visibility_type"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	err := c.SetAffiliateOfferVisibility(context.Background(), 7, []int64{67}, "bogus")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %T %v", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400", apiErr.Status)
	}
	if apiErr.Message != "invalid visibility_type" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "invalid visibility_type")
	}
}
