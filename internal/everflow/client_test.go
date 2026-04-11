// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_DoRequest_SetsAuthAndUserAgent(t *testing.T) {
	t.Parallel()

	var (
		gotAuth        string
		gotUA          string
		gotAccept      string
		gotContentType string
		gotMethod      string
		gotPath        string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Eflow-Api-Key")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := New("secret-key", srv.URL, "test")
	var out struct {
		OK bool `json:"ok"`
	}
	err := c.DoRequest(context.Background(), http.MethodPost, "/v1/networks/advertisers", map[string]string{"name": "x"}, &out)
	if err != nil {
		t.Fatalf("DoRequest returned error: %v", err)
	}

	if gotAuth != "secret-key" {
		t.Errorf("X-Eflow-Api-Key = %q, want %q", gotAuth, "secret-key")
	}
	if gotUA != "terraform-provider-everflow/test" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "terraform-provider-everflow/test")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("Method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/networks/advertisers" {
		t.Errorf("Path = %q, want /v1/networks/advertisers", gotPath)
	}
	if !out.OK {
		t.Errorf("decoded response: ok = false, want true")
	}
}

func TestClient_New_DefaultsBaseURL(t *testing.T) {
	t.Parallel()

	c := New("k", "", "v")
	if c.baseURL.String() != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL.String(), DefaultBaseURL)
	}
}

func TestClient_DoRequest_DecodesEverflowErrorShape(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"name is required"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	err := c.DoRequest(context.Background(), http.MethodPost, "/v1/networks/advertisers", map[string]string{}, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %T %v", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", apiErr.Status, http.StatusBadRequest)
	}
	if apiErr.Message != "name is required" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "name is required")
	}
	if apiErr.RawBody == "" {
		t.Errorf("RawBody is empty; want raw JSON preserved for logs")
	}
}

func TestClient_DoRequest_404IsNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"Error":"not found"}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	err := c.DoRequest(context.Background(), http.MethodGet, "/v1/networks/advertisers/123", nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, want true (err=%v)", err)
	}
}

func TestClient_DoRequest_NoBodyOmitsContentType(t *testing.T) {
	t.Parallel()

	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New("k", srv.URL, "test")
	err := c.DoRequest(context.Background(), http.MethodDelete, "/v1/networks/advertisers/1", nil, nil)
	if err != nil {
		t.Fatalf("DoRequest returned error: %v", err)
	}
	if gotContentType != "" {
		t.Errorf("Content-Type = %q for nil body, want empty", gotContentType)
	}
}

func TestAPIError_ErrorStringWithAndWithoutMessage(t *testing.T) {
	t.Parallel()

	withMsg := (&APIError{Status: 400, Message: "bad"}).Error()
	if withMsg != "everflow: 400 bad" {
		t.Errorf("with-message Error() = %q", withMsg)
	}

	withoutMsg := (&APIError{Status: 500}).Error()
	if withoutMsg != "everflow: 500 Internal Server Error" {
		t.Errorf("without-message Error() = %q", withoutMsg)
	}
}
