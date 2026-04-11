// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

// Package everflow is a thin, typed HTTP client for the Everflow Network API.
//
// The package is deliberately agnostic of Terraform: it knows how to speak
// JSON to https://api.eflow.team and nothing more. Resource-specific types
// and CRUD helpers live in sibling files (e.g. advertiser.go, offer.go) that
// all use the shared Client below.
package everflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the default Everflow Network API base URL. The
	// provider's base_url attribute overrides this for test environments.
	DefaultBaseURL = "https://api.eflow.team"

	// authHeader is the HTTP header Everflow expects for API key auth.
	// Capitalization matches the Everflow docs verbatim.
	authHeader = "X-Eflow-Api-Key"

	// defaultTimeout bounds a single round-trip end-to-end. Everflow's API
	// is generally fast; anything slower than this is almost certainly a
	// network issue rather than a legitimate long-running request.
	defaultTimeout = 30 * time.Second
)

// Client is a thin HTTP client for the Everflow Network API. It is safe for
// concurrent use by multiple goroutines.
type Client struct {
	httpClient *http.Client
	baseURL    *url.URL
	apiKey     string
	userAgent  string
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client. Primarily useful in
// tests to inject a custom transport (e.g. httptest.Server).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// New constructs a Client bound to the given API key and base URL. If
// baseURL is empty, DefaultBaseURL is used. The version string is embedded
// in the User-Agent header for observability on the Everflow side.
func New(apiKey, baseURL, version string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		// url.Parse is extremely permissive; the only way to trip this is
		// with a control-character-laden string. Fall back to the default
		// rather than panicking so a typo in provider config surfaces as an
		// API error at request time instead of at plugin startup.
		u, _ = url.Parse(DefaultBaseURL)
	}

	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    u,
		apiKey:     apiKey,
		userAgent:  fmt.Sprintf("terraform-provider-everflow/%s", version),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DoRequest performs an authenticated HTTP request against the Everflow API.
//
//   - path is joined onto the configured base URL
//     (e.g. "/v1/networks/advertisers")
//   - body, if non-nil, is JSON-encoded and sent as the request body
//   - out, if non-nil, receives the JSON-decoded response
//
// Non-2xx responses are returned as a typed *APIError. Callers can use
// IsNotFound to detect 404s, which is the standard "remove from state"
// signal used by the resource Read implementations.
func (c *Client) DoRequest(ctx context.Context, method, path string, body, out any) error {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("everflow: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("everflow: build request: %w", err)
	}
	req.Header.Set(authHeader, c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("everflow: http transport: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return decodeAPIError(resp)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		// Drain the body so the keep-alive connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("everflow: decode response: %w", err)
	}
	return nil
}
