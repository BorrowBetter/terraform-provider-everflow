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

func TestCreateOffer_PostsExpectedBody(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_offer_id": 77,
			"network_id": 1,
			"name": "Acme Offer",
			"network_advertiser_id": 42,
			"destination_url": "https://example.com/landing",
			"offer_status": "active",
			"currency_id": "USD",
			"conversion_method": "server_postback",
			"network_tracking_domain_id": 5,
			"internal_notes": "hello",
			"payout_revenue": [
				{
					"entry_name": "Base",
					"payout_type": "cpa",
					"payout_amount": 5.00,
					"revenue_type": "rpa",
					"revenue_amount": 10.00,
					"is_default": true,
					"is_private": false
				}
			],
			"time_created": 1700000000,
			"time_saved": 1700000001
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.CreateOffer(context.Background(), CreateOfferInput{
		Name:                    "Acme Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
		InternalNotes:           "hello",
		PayoutRevenue: []PayoutRevenueEntry{{
			EntryName:     "Base",
			PayoutType:    "cpa",
			PayoutAmount:  5.00,
			RevenueType:   "rpa",
			RevenueAmount: 10.00,
			IsDefault:     true,
			IsPrivate:     false,
		}},
	})
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/networks/offers" {
		t.Errorf("path = %q, want /v1/networks/offers", gotPath)
	}
	if gotBody["name"] != "Acme Offer" {
		t.Errorf("body.name = %v, want Acme Offer", gotBody["name"])
	}
	if gotBody["network_advertiser_id"].(float64) != 42 {
		t.Errorf("body.network_advertiser_id = %v, want 42", gotBody["network_advertiser_id"])
	}
	if gotBody["destination_url"] != "https://example.com/landing" {
		t.Errorf("body.destination_url = %v, want https://example.com/landing", gotBody["destination_url"])
	}
	if gotBody["offer_status"] != "active" {
		t.Errorf("body.offer_status = %v, want active", gotBody["offer_status"])
	}
	if gotBody["currency_id"] != "USD" {
		t.Errorf("body.currency_id = %v, want USD", gotBody["currency_id"])
	}
	if gotBody["conversion_method"] != "server_postback" {
		t.Errorf("body.conversion_method = %v, want server_postback", gotBody["conversion_method"])
	}
	if gotBody["network_tracking_domain_id"].(float64) != 5 {
		t.Errorf("body.network_tracking_domain_id = %v, want 5", gotBody["network_tracking_domain_id"])
	}
	if gotBody["internal_notes"] != "hello" {
		t.Errorf("body.internal_notes = %v, want hello", gotBody["internal_notes"])
	}

	// payout_revenue must round-trip as a JSON array with the expected
	// scalar shape. This is the main structural difference from the
	// advertiser/affiliate resources and is the contract we lock in here.
	payouts, ok := gotBody["payout_revenue"].([]any)
	if !ok || len(payouts) != 1 {
		t.Fatalf("body.payout_revenue = %v, want 1-element array", gotBody["payout_revenue"])
	}
	p0, ok := payouts[0].(map[string]any)
	if !ok {
		t.Fatalf("payout_revenue[0] = %T, want map", payouts[0])
	}
	if p0["payout_type"] != "cpa" {
		t.Errorf("payout_revenue[0].payout_type = %v, want cpa", p0["payout_type"])
	}
	if p0["revenue_type"] != "rpa" {
		t.Errorf("payout_revenue[0].revenue_type = %v, want rpa", p0["revenue_type"])
	}
	if p0["is_default"] != true {
		t.Errorf("payout_revenue[0].is_default = %v, want true", p0["is_default"])
	}
	if p0["payout_amount"].(float64) != 5.00 {
		t.Errorf("payout_revenue[0].payout_amount = %v, want 5", p0["payout_amount"])
	}

	if got.NetworkOfferID != 77 {
		t.Errorf("resp.NetworkOfferID = %d, want 77", got.NetworkOfferID)
	}
	if got.TimeCreated != 1700000000 {
		t.Errorf("resp.TimeCreated = %d, want 1700000000", got.TimeCreated)
	}
	if len(got.PayoutRevenue) != 1 || got.PayoutRevenue[0].PayoutType != "cpa" {
		t.Errorf("resp.PayoutRevenue = %+v, want one cpa entry", got.PayoutRevenue)
	}
}

func TestCreateOffer_OmitsInternalNotesWhenEmpty(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network_offer_id":1}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateOffer(context.Background(), CreateOfferInput{
		Name:                    "Acme Offer",
		NetworkAdvertiserID:     42,
		DestinationURL:          "https://example.com/landing",
		OfferStatus:             "active",
		CurrencyID:              "USD",
		ConversionMethod:        "server_postback",
		NetworkTrackingDomainID: 5,
		PayoutRevenue: []PayoutRevenueEntry{{
			PayoutType:  "cpa",
			RevenueType: "rpa",
			IsDefault:   true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}

	if _, present := gotBody["internal_notes"]; present {
		t.Errorf("internal_notes should be omitted when empty, got %v", gotBody["internal_notes"])
	}
	// Even when callers don't opt into optional per-entry fields, the
	// required is_default flag must still land in the body as a literal
	// false/true rather than being dropped entirely. (Only bool zero
	// values with omitempty would drop; the tag is intentionally absent.)
	payouts, ok := gotBody["payout_revenue"].([]any)
	if !ok || len(payouts) != 1 {
		t.Fatalf("payout_revenue missing in body: %v", gotBody["payout_revenue"])
	}
	p0 := payouts[0].(map[string]any)
	if p0["is_default"] != true {
		t.Errorf("payout_revenue[0].is_default = %v, want true", p0["is_default"])
	}
	// Optional entry fields should omit when zero.
	if _, ok := p0["entry_name"]; ok {
		t.Errorf("entry_name should be omitted when empty, got %v", p0["entry_name"])
	}
	if _, ok := p0["payout_amount"]; ok {
		t.Errorf("payout_amount should be omitted when zero, got %v", p0["payout_amount"])
	}
}

func TestGetOffer_DecodesTypedResponse(t *testing.T) {
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
			"network_offer_id": 77,
			"network_id": 1,
			"name": "Acme Offer",
			"network_advertiser_id": 42,
			"destination_url": "https://example.com/landing",
			"offer_status": "active",
			"currency_id": "USD",
			"conversion_method": "server_postback",
			"network_tracking_domain_id": 5,
			"internal_notes": "hello",
			"payout_revenue": [
				{
					"entry_name": "Base",
					"payout_type": "cpa",
					"payout_amount": 5.00,
					"revenue_type": "rpa",
					"revenue_amount": 10.00,
					"is_default": true,
					"is_private": false
				}
			]
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetOffer(context.Background(), 77)
	if err != nil {
		t.Fatalf("GetOffer returned error: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/networks/offers/77" {
		t.Errorf("path = %q, want /v1/networks/offers/77", gotPath)
	}
	if got.Name != "Acme Offer" {
		t.Errorf("Name = %q, want Acme Offer", got.Name)
	}
	if got.InternalNotes != "hello" {
		t.Errorf("InternalNotes = %q, want hello", got.InternalNotes)
	}
	if len(got.PayoutRevenue) != 1 {
		t.Fatalf("len(PayoutRevenue) = %d, want 1", len(got.PayoutRevenue))
	}
	p := got.PayoutRevenue[0]
	if p.PayoutType != "cpa" || p.RevenueType != "rpa" || !p.IsDefault {
		t.Errorf("PayoutRevenue[0] = %+v, want cpa/rpa/default", p)
	}
}

func TestGetOfferRaw_PreservesUnknownFields(t *testing.T) {
	t.Parallel()

	// Simulates Everflow returning nested objects (ruleset, traffic_filters,
	// labels) that the typed Offer deliberately does not model. The raw
	// map must retain them so fetch-modify-put can round-trip them on
	// Update.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_offer_id": 77,
			"name": "Acme Offer",
			"offer_status": "active",
			"ruleset": {
				"countries": ["US", "CA"],
				"platform_targeting": "mobile"
			},
			"traffic_filters": [
				{"type": "device_type", "value": "mobile"}
			],
			"labels": ["vip", "partner"]
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetOfferRaw(context.Background(), 77)
	if err != nil {
		t.Fatalf("GetOfferRaw returned error: %v", err)
	}

	if got["name"] != "Acme Offer" {
		t.Errorf("name = %v, want Acme Offer", got["name"])
	}
	ruleset, ok := got["ruleset"].(map[string]any)
	if !ok {
		t.Fatalf("ruleset not preserved as nested object: %T %v", got["ruleset"], got["ruleset"])
	}
	if ruleset["platform_targeting"] != "mobile" {
		t.Errorf("ruleset.platform_targeting = %v, want mobile", ruleset["platform_targeting"])
	}
	tf, ok := got["traffic_filters"].([]any)
	if !ok || len(tf) != 1 {
		t.Errorf("traffic_filters = %v, want 1-element slice", got["traffic_filters"])
	}
	labels, ok := got["labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Errorf("labels = %v, want 2-element slice", got["labels"])
	}
}

func TestUpdateOffer_PutsMergedBody(t *testing.T) {
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

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_offer_id": 77,
			"name": "Acme Renamed",
			"offer_status": "active",
			"network_advertiser_id": 42,
			"destination_url": "https://example.com/landing",
			"currency_id": "USD",
			"conversion_method": "server_postback",
			"network_tracking_domain_id": 5
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")

	// Simulate the overlay the resource's Update would produce: fetched
	// raw body + schema-managed field writes, including the modeled
	// payout_revenue array and an unmodeled ruleset nested object that
	// must survive the round trip untouched.
	merged := map[string]any{
		"network_offer_id":           float64(77),
		"name":                       "Acme Renamed",
		"network_advertiser_id":      float64(42),
		"destination_url":            "https://example.com/landing",
		"offer_status":               "active",
		"currency_id":                "USD",
		"conversion_method":          "server_postback",
		"network_tracking_domain_id": float64(5),
		"payout_revenue": []map[string]any{
			{
				"payout_type":    "cpa",
				"payout_amount":  5.00,
				"revenue_type":   "rpa",
				"revenue_amount": 10.00,
				"is_default":     true,
				"is_private":     false,
			},
		},
		"ruleset": map[string]any{
			"countries": []any{"US", "CA"},
		},
	}
	got, err := c.UpdateOffer(context.Background(), 77, merged)
	if err != nil {
		t.Fatalf("UpdateOffer returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/v1/networks/offers/77" {
		t.Errorf("path = %q, want /v1/networks/offers/77", gotPath)
	}
	if gotBody["name"] != "Acme Renamed" {
		t.Errorf("body.name = %v, want Acme Renamed", gotBody["name"])
	}
	// The modeled payout_revenue must survive the round trip.
	payouts, ok := gotBody["payout_revenue"].([]any)
	if !ok || len(payouts) != 1 {
		t.Fatalf("body.payout_revenue = %v, want 1-element array", gotBody["payout_revenue"])
	}
	// Unmodeled nested object must also survive the round trip — this
	// is the fetch-modify-put preservation contract for offers.
	ruleset, ok := gotBody["ruleset"].(map[string]any)
	if !ok {
		t.Fatalf("ruleset not forwarded in PUT body: %T %v", gotBody["ruleset"], gotBody["ruleset"])
	}
	countries, ok := ruleset["countries"].([]any)
	if !ok || len(countries) != 2 {
		t.Errorf("ruleset.countries not forwarded: %v", ruleset["countries"])
	}

	if got.Name != "Acme Renamed" {
		t.Errorf("resp.Name = %q, want Acme Renamed", got.Name)
	}
}

func TestOffer_NotFoundBubblesUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"Error":"not found"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.GetOffer(context.Background(), 99)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, want true (err=%v)", err)
	}
}

func TestOffer_BadRequestDecodesAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"network_tracking_domain_id is required"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateOffer(context.Background(), CreateOfferInput{Name: "x"})
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
	if apiErr.Message != "network_tracking_domain_id is required" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "network_tracking_domain_id is required")
	}
}
