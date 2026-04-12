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

func TestCreateAffiliate_PostsExpectedBody(t *testing.T) {
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
			"network_affiliate_id": 42,
			"network_id": 1,
			"name": "Acme Affiliate",
			"account_status": "active",
			"network_employee_id": 11,
			"default_currency_id": "USD",
			"internal_notes": "hello",
			"time_created": 1700000000,
			"time_saved": 1700000001
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.CreateAffiliate(context.Background(), CreateAffiliateInput{
		Name:              "Acme Affiliate",
		AccountStatus:     "active",
		NetworkEmployeeID: 11,
		DefaultCurrencyID: "USD",
		InternalNotes:     "hello",
		Billing: AffiliateBilling{
			BillingFrequency: "monthly",
			PaymentType:      "none",
			Details:          AffiliateBillingDetails{DayOfMonth: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreateAffiliate returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/networks/affiliates" {
		t.Errorf("path = %q, want /v1/networks/affiliates", gotPath)
	}
	if gotBody["name"] != "Acme Affiliate" {
		t.Errorf("body.name = %v, want Acme Affiliate", gotBody["name"])
	}
	if gotBody["account_status"] != "active" {
		t.Errorf("body.account_status = %v, want active", gotBody["account_status"])
	}
	if gotBody["network_employee_id"].(float64) != 11 {
		t.Errorf("body.network_employee_id = %v, want 11", gotBody["network_employee_id"])
	}
	if gotBody["default_currency_id"] != "USD" {
		t.Errorf("body.default_currency_id = %v, want USD", gotBody["default_currency_id"])
	}
	// reporting_timezone_id is intentionally NOT a top-level field on
	// affiliates — it lives inside nested user objects.
	if _, present := gotBody["reporting_timezone_id"]; present {
		t.Errorf("body should not include reporting_timezone_id for affiliates, got %v", gotBody["reporting_timezone_id"])
	}
	if gotBody["internal_notes"] != "hello" {
		t.Errorf("body.internal_notes = %v, want hello", gotBody["internal_notes"])
	}
	// Billing block must be present in the POST body — this is the fix
	// for #13 (affiliate creation fails without billing).
	billing, ok := gotBody["billing"].(map[string]any)
	if !ok {
		t.Fatalf("body.billing missing or not an object: %v", gotBody["billing"])
	}
	if billing["billing_frequency"] != "monthly" {
		t.Errorf("body.billing.billing_frequency = %v, want monthly", billing["billing_frequency"])
	}
	if billing["payment_type"] != "none" {
		t.Errorf("body.billing.payment_type = %v, want none", billing["payment_type"])
	}
	details, ok := billing["details"].(map[string]any)
	if !ok {
		t.Fatalf("body.billing.details missing or not an object: %v", billing["details"])
	}
	if details["day_of_month"].(float64) != 1 {
		t.Errorf("body.billing.details.day_of_month = %v, want 1", details["day_of_month"])
	}

	if got.NetworkAffiliateID != 42 {
		t.Errorf("resp.NetworkAffiliateID = %d, want 42", got.NetworkAffiliateID)
	}
	if got.TimeCreated != 1700000000 {
		t.Errorf("resp.TimeCreated = %d, want 1700000000", got.TimeCreated)
	}
}

func TestCreateAffiliate_OmitsInternalNotesWhenEmpty(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network_affiliate_id":1}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateAffiliate(context.Background(), CreateAffiliateInput{
		Name:              "Acme Affiliate",
		AccountStatus:     "active",
		NetworkEmployeeID: 1,
		DefaultCurrencyID: "USD",
		Billing: AffiliateBilling{
			BillingFrequency: "monthly",
			PaymentType:      "none",
			Details:          AffiliateBillingDetails{DayOfMonth: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreateAffiliate returned error: %v", err)
	}

	if _, present := gotBody["internal_notes"]; present {
		t.Errorf("internal_notes should be omitted when empty, got %v", gotBody["internal_notes"])
	}
}

func TestGetAffiliate_DecodesTypedResponse(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// Billing is intentionally absent — the real Everflow API does
		// NOT return billing on GET (write-only field).
		_, _ = w.Write([]byte(`{
			"network_affiliate_id": 42,
			"network_id": 1,
			"name": "Acme Affiliate",
			"account_status": "active",
			"network_employee_id": 11,
			"default_currency_id": "USD",
			"internal_notes": "hello"
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetAffiliate(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetAffiliate returned error: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/networks/affiliates/42" {
		t.Errorf("path = %q, want /v1/networks/affiliates/42", gotPath)
	}
	if got.Name != "Acme Affiliate" {
		t.Errorf("Name = %q, want Acme Affiliate", got.Name)
	}
	if got.InternalNotes != "hello" {
		t.Errorf("InternalNotes = %q, want hello", got.InternalNotes)
	}
}

func TestGetAffiliateRaw_PreservesUnknownFields(t *testing.T) {
	t.Parallel()

	// Simulates Everflow returning nested objects (billing, labels) that
	// the typed Affiliate deliberately does not model. The raw map must
	// retain them so fetch-modify-put can round-trip them on Update.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_affiliate_id": 42,
			"name": "Acme Affiliate",
			"account_status": "active",
			"billing": {
				"payment_type": "wire",
				"default_payment_terms": 30
			},
			"labels": ["vip", "partner"]
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetAffiliateRaw(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetAffiliateRaw returned error: %v", err)
	}

	if got["name"] != "Acme Affiliate" {
		t.Errorf("name = %v, want Acme Affiliate", got["name"])
	}
	billing, ok := got["billing"].(map[string]any)
	if !ok {
		t.Fatalf("billing not preserved as nested object: %T %v", got["billing"], got["billing"])
	}
	if billing["payment_type"] != "wire" {
		t.Errorf("billing.payment_type = %v, want wire", billing["payment_type"])
	}
	labels, ok := got["labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Errorf("labels = %v, want 2-element slice", got["labels"])
	}
}

func TestUpdateAffiliate_PutsMergedBody(t *testing.T) {
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
			"network_affiliate_id": 42,
			"name": "Acme Renamed",
			"account_status": "active",
			"network_employee_id": 11,
			"default_currency_id": "USD"
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")

	// Simulate the overlay the resource's Update would produce: fetched raw
	// body + schema-managed field writes.
	merged := map[string]any{
		"network_affiliate_id": float64(42),
		"name":                 "Acme Renamed",
		"account_status":       "active",
		"network_employee_id":  float64(11),
		"default_currency_id":  "USD",
		"billing": map[string]any{
			"payment_type": "wire",
		},
		"labels": []any{"vip"},
	}
	got, err := c.UpdateAffiliate(context.Background(), 42, merged)
	if err != nil {
		t.Fatalf("UpdateAffiliate returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/v1/networks/affiliates/42" {
		t.Errorf("path = %q, want /v1/networks/affiliates/42", gotPath)
	}
	if gotBody["name"] != "Acme Renamed" {
		t.Errorf("body.name = %v, want Acme Renamed", gotBody["name"])
	}
	// Unmodeled field must survive the round trip.
	billing, ok := gotBody["billing"].(map[string]any)
	if !ok {
		t.Fatalf("billing not forwarded in PUT body: %T %v", gotBody["billing"], gotBody["billing"])
	}
	if billing["payment_type"] != "wire" {
		t.Errorf("billing.payment_type = %v, want wire", billing["payment_type"])
	}
	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 1 || labels[0] != "vip" {
		t.Errorf("labels not forwarded in PUT body: %v", gotBody["labels"])
	}

	if got.Name != "Acme Renamed" {
		t.Errorf("resp.Name = %q, want Acme Renamed", got.Name)
	}
}

func TestAffiliate_NotFoundBubblesUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"Error":"not found"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.GetAffiliate(context.Background(), 99)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, want true (err=%v)", err)
	}
}

func TestAffiliate_BadRequestDecodesAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"network_employee_id is required"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateAffiliate(context.Background(), CreateAffiliateInput{Name: "x"})
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
	if apiErr.Message != "network_employee_id is required" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "network_employee_id is required")
	}
}
