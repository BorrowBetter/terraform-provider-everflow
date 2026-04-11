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

func TestCreateAdvertiser_PostsExpectedBody(t *testing.T) {
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
			"network_advertiser_id": 42,
			"network_id": 1,
			"name": "Acme",
			"account_status": "active",
			"network_employee_id": 11,
			"default_currency_id": "USD",
			"reporting_timezone_id": 80,
			"internal_notes": "hello",
			"time_created": 1700000000,
			"time_saved": 1700000001
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.CreateAdvertiser(context.Background(), CreateAdvertiserInput{
		Name:                "Acme",
		AccountStatus:       "active",
		NetworkEmployeeID:   11,
		DefaultCurrencyID:   "USD",
		ReportingTimezoneID: 80,
		InternalNotes:       "hello",
	})
	if err != nil {
		t.Fatalf("CreateAdvertiser returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/networks/advertisers" {
		t.Errorf("path = %q, want /v1/networks/advertisers", gotPath)
	}
	if gotBody["name"] != "Acme" {
		t.Errorf("body.name = %v, want Acme", gotBody["name"])
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
	if gotBody["reporting_timezone_id"].(float64) != 80 {
		t.Errorf("body.reporting_timezone_id = %v, want 80", gotBody["reporting_timezone_id"])
	}
	if gotBody["internal_notes"] != "hello" {
		t.Errorf("body.internal_notes = %v, want hello", gotBody["internal_notes"])
	}

	if got.NetworkAdvertiserID != 42 {
		t.Errorf("resp.NetworkAdvertiserID = %d, want 42", got.NetworkAdvertiserID)
	}
	if got.TimeCreated != 1700000000 {
		t.Errorf("resp.TimeCreated = %d, want 1700000000", got.TimeCreated)
	}
}

func TestCreateAdvertiser_OmitsInternalNotesWhenEmpty(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network_advertiser_id":1}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateAdvertiser(context.Background(), CreateAdvertiserInput{
		Name:                "Acme",
		AccountStatus:       "active",
		NetworkEmployeeID:   1,
		DefaultCurrencyID:   "USD",
		ReportingTimezoneID: 80,
	})
	if err != nil {
		t.Fatalf("CreateAdvertiser returned error: %v", err)
	}

	if _, present := gotBody["internal_notes"]; present {
		t.Errorf("internal_notes should be omitted when empty, got %v", gotBody["internal_notes"])
	}
}

func TestGetAdvertiser_DecodesTypedResponse(t *testing.T) {
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
			"network_advertiser_id": 42,
			"network_id": 1,
			"name": "Acme",
			"account_status": "active",
			"network_employee_id": 11,
			"default_currency_id": "USD",
			"reporting_timezone_id": 80,
			"internal_notes": "hello"
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetAdvertiser(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetAdvertiser returned error: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/networks/advertisers/42" {
		t.Errorf("path = %q, want /v1/networks/advertisers/42", gotPath)
	}
	if got.Name != "Acme" {
		t.Errorf("Name = %q, want Acme", got.Name)
	}
	if got.InternalNotes != "hello" {
		t.Errorf("InternalNotes = %q, want hello", got.InternalNotes)
	}
}

func TestGetAdvertiserRaw_PreservesUnknownFields(t *testing.T) {
	t.Parallel()

	// Simulates Everflow returning nested objects (billing, settings) that
	// the typed Advertiser deliberately does not model. The raw map must
	// retain them so fetch-modify-put can round-trip them on Update.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_advertiser_id": 42,
			"name": "Acme",
			"account_status": "active",
			"billing": {
				"billing_frequency": "monthly",
				"default_payment_terms": 30
			},
			"settings": {
				"exposed_variables": ["adv1", "adv2"]
			}
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetAdvertiserRaw(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetAdvertiserRaw returned error: %v", err)
	}

	if got["name"] != "Acme" {
		t.Errorf("name = %v, want Acme", got["name"])
	}
	billing, ok := got["billing"].(map[string]any)
	if !ok {
		t.Fatalf("billing not preserved as nested object: %T %v", got["billing"], got["billing"])
	}
	if billing["billing_frequency"] != "monthly" {
		t.Errorf("billing.billing_frequency = %v, want monthly", billing["billing_frequency"])
	}
	settings, ok := got["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings not preserved as nested object: %T %v", got["settings"], got["settings"])
	}
	exposed, ok := settings["exposed_variables"].([]any)
	if !ok || len(exposed) != 2 {
		t.Errorf("settings.exposed_variables = %v, want 2-element slice", settings["exposed_variables"])
	}
}

func TestUpdateAdvertiser_PutsMergedBody(t *testing.T) {
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
			"network_advertiser_id": 42,
			"name": "Acme Renamed",
			"account_status": "active",
			"network_employee_id": 11,
			"default_currency_id": "USD",
			"reporting_timezone_id": 80
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")

	// Simulate the overlay the resource's Update would produce: fetched raw
	// body + schema-managed field writes.
	merged := map[string]any{
		"network_advertiser_id": float64(42),
		"name":                  "Acme Renamed",
		"account_status":        "active",
		"network_employee_id":   float64(11),
		"default_currency_id":   "USD",
		"reporting_timezone_id": float64(80),
		"billing": map[string]any{
			"billing_frequency": "monthly",
		},
	}
	got, err := c.UpdateAdvertiser(context.Background(), 42, merged)
	if err != nil {
		t.Fatalf("UpdateAdvertiser returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/v1/networks/advertisers/42" {
		t.Errorf("path = %q, want /v1/networks/advertisers/42", gotPath)
	}
	if gotBody["name"] != "Acme Renamed" {
		t.Errorf("body.name = %v, want Acme Renamed", gotBody["name"])
	}
	// Unmodeled field must survive the round trip.
	billing, ok := gotBody["billing"].(map[string]any)
	if !ok {
		t.Fatalf("billing not forwarded in PUT body: %T %v", gotBody["billing"], gotBody["billing"])
	}
	if billing["billing_frequency"] != "monthly" {
		t.Errorf("billing.billing_frequency = %v, want monthly", billing["billing_frequency"])
	}

	if got.Name != "Acme Renamed" {
		t.Errorf("resp.Name = %q, want Acme Renamed", got.Name)
	}
}

func TestAdvertiser_NotFoundBubblesUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"Error":"not found"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.GetAdvertiser(context.Background(), 99)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, want true (err=%v)", err)
	}
}

func TestAdvertiser_BadRequestDecodesAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"network_employee_id is required"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateAdvertiser(context.Background(), CreateAdvertiserInput{Name: "x"})
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
