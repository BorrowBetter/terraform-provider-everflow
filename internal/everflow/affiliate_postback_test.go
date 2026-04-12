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

func TestCreateAffiliatePostback_PostsExpectedBody(t *testing.T) {
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
			"network_pixel_id": 3,
			"network_id": 1,
			"network_affiliate_id": 7,
			"delivery_method": "postback",
			"pixel_level": "global",
			"pixel_type": "conversion",
			"pixel_status": "active",
			"postback_url": "https://example.com/postback?tid={transaction_id}",
			"delay_ms": 0,
			"description": "Global postback",
			"time_created": 1700000000,
			"time_saved": 1700000001
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.CreateAffiliatePostback(context.Background(), CreateAffiliatePostbackInput{
		NetworkAffiliateID: 7,
		DeliveryMethod:     "postback",
		PixelLevel:         "global",
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback?tid={transaction_id}",
		Description:        "Global postback",
	})
	if err != nil {
		t.Fatalf("CreateAffiliatePostback returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/networks/pixels" {
		t.Errorf("path = %q, want /v1/networks/pixels", gotPath)
	}
	if gotBody["network_affiliate_id"].(float64) != 7 {
		t.Errorf("body.network_affiliate_id = %v, want 7", gotBody["network_affiliate_id"])
	}
	if gotBody["delivery_method"] != "postback" {
		t.Errorf("body.delivery_method = %v, want postback", gotBody["delivery_method"])
	}
	if gotBody["pixel_level"] != "global" {
		t.Errorf("body.pixel_level = %v, want global", gotBody["pixel_level"])
	}
	if gotBody["pixel_type"] != "conversion" {
		t.Errorf("body.pixel_type = %v, want conversion", gotBody["pixel_type"])
	}
	if gotBody["postback_url"] != "https://example.com/postback?tid={transaction_id}" {
		t.Errorf("body.postback_url = %v, want URL with macros", gotBody["postback_url"])
	}
	if gotBody["description"] != "Global postback" {
		t.Errorf("body.description = %v, want 'Global postback'", gotBody["description"])
	}

	if got.NetworkPixelID != 3 {
		t.Errorf("resp.NetworkPixelID = %d, want 3", got.NetworkPixelID)
	}
	if got.NetworkAffiliateID != 7 {
		t.Errorf("resp.NetworkAffiliateID = %d, want 7", got.NetworkAffiliateID)
	}
	if got.PostbackURL != "https://example.com/postback?tid={transaction_id}" {
		t.Errorf("resp.PostbackURL = %q, want URL with macros", got.PostbackURL)
	}
	if got.TimeCreated != 1700000000 {
		t.Errorf("resp.TimeCreated = %d, want 1700000000", got.TimeCreated)
	}
}

func TestCreateAffiliatePostback_OmitsDescriptionWhenEmpty(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network_pixel_id":1}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateAffiliatePostback(context.Background(), CreateAffiliatePostbackInput{
		NetworkAffiliateID: 7,
		DeliveryMethod:     "postback",
		PixelLevel:         "global",
		PixelType:          "conversion",
		PixelStatus:        "active",
		PostbackURL:        "https://example.com/postback",
	})
	if err != nil {
		t.Fatalf("CreateAffiliatePostback returned error: %v", err)
	}

	if _, present := gotBody["description"]; present {
		t.Errorf("description should be omitted when empty, got %v", gotBody["description"])
	}
}

func TestGetAffiliatePostback_DecodesTypedResponse(t *testing.T) {
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
			"network_pixel_id": 3,
			"network_id": 1,
			"network_affiliate_id": 7,
			"delivery_method": "postback",
			"pixel_level": "global",
			"pixel_type": "conversion",
			"pixel_status": "active",
			"postback_url": "https://example.com/postback",
			"description": "hello"
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetAffiliatePostback(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetAffiliatePostback returned error: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/networks/pixels/3" {
		t.Errorf("path = %q, want /v1/networks/pixels/3", gotPath)
	}
	if got.NetworkPixelID != 3 {
		t.Errorf("NetworkPixelID = %d, want 3", got.NetworkPixelID)
	}
	if got.PostbackURL != "https://example.com/postback" {
		t.Errorf("PostbackURL = %q, want https://example.com/postback", got.PostbackURL)
	}
	if got.Description != "hello" {
		t.Errorf("Description = %q, want hello", got.Description)
	}
}

func TestGetAffiliatePostbackRaw_PreservesUnknownFields(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"network_pixel_id": 3,
			"network_affiliate_id": 7,
			"postback_url": "https://example.com/postback",
			"some_future_field": {"nested": true},
			"pixel_options": ["retry"]
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	got, err := c.GetAffiliatePostbackRaw(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetAffiliatePostbackRaw returned error: %v", err)
	}

	if got["postback_url"] != "https://example.com/postback" {
		t.Errorf("postback_url = %v, want https://example.com/postback", got["postback_url"])
	}
	nested, ok := got["some_future_field"].(map[string]any)
	if !ok {
		t.Fatalf("some_future_field not preserved: %T %v", got["some_future_field"], got["some_future_field"])
	}
	if nested["nested"] != true {
		t.Errorf("some_future_field.nested = %v, want true", nested["nested"])
	}
	opts, ok := got["pixel_options"].([]any)
	if !ok || len(opts) != 1 {
		t.Errorf("pixel_options = %v, want 1-element slice", got["pixel_options"])
	}
}

func TestUpdateAffiliatePostback_PutsMergedBody(t *testing.T) {
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
			"network_pixel_id": 3,
			"network_id": 1,
			"network_affiliate_id": 7,
			"delivery_method": "postback",
			"pixel_level": "global",
			"pixel_type": "conversion",
			"pixel_status": "active",
			"postback_url": "https://example.com/updated"
		}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	merged := map[string]any{
		"network_pixel_id":     float64(3),
		"network_affiliate_id": float64(7),
		"delivery_method":      "postback",
		"pixel_level":          "global",
		"pixel_type":           "conversion",
		"pixel_status":         "active",
		"postback_url":         "https://example.com/updated",
		"some_future_field":    map[string]any{"nested": true},
	}
	got, err := c.UpdateAffiliatePostback(context.Background(), 3, merged)
	if err != nil {
		t.Fatalf("UpdateAffiliatePostback returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/v1/networks/pixels/3" {
		t.Errorf("path = %q, want /v1/networks/pixels/3", gotPath)
	}
	if gotBody["postback_url"] != "https://example.com/updated" {
		t.Errorf("body.postback_url = %v, want updated URL", gotBody["postback_url"])
	}
	// Unmodeled fields must survive.
	future, ok := gotBody["some_future_field"].(map[string]any)
	if !ok {
		t.Fatalf("some_future_field not forwarded in PUT body: %T %v", gotBody["some_future_field"], gotBody["some_future_field"])
	}
	if future["nested"] != true {
		t.Errorf("some_future_field.nested not forwarded: %v", future["nested"])
	}
	if got.PostbackURL != "https://example.com/updated" {
		t.Errorf("resp.PostbackURL = %q, want updated URL", got.PostbackURL)
	}
}

func TestAffiliatePostback_NotFoundBubblesUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"Error":"not found"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.GetAffiliatePostback(context.Background(), 99)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, want true (err=%v)", err)
	}
}

func TestAffiliatePostback_BadRequestDecodesAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"postback_url is required"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	_, err := c.CreateAffiliatePostback(context.Background(), CreateAffiliatePostbackInput{
		NetworkAffiliateID: 7,
		DeliveryMethod:     "postback",
		PixelLevel:         "global",
		PixelType:          "conversion",
		PixelStatus:        "active",
	})
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
	if apiErr.Message != "postback_url is required" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "postback_url is required")
	}
}
