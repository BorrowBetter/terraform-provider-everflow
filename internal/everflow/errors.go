// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// APIError is returned by the Client for non-2xx responses. Status is the
// raw HTTP status code; Message is the decoded error text from Everflow's
// response (which uses a top-level capitalized "Error" key — note the
// capital E, this is an Everflow quirk and not standard JSON-API).
type APIError struct {
	Status  int
	Message string
	// RawBody preserves the undecoded response body so callers (and logs)
	// can inspect non-standard error shapes when decoding falls through.
	RawBody string
}

// Error renders the APIError in a form suitable for both user-facing
// diagnostics and debug logs.
func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("everflow: %d %s", e.Status, e.Message)
	}
	return fmt.Sprintf("everflow: %d %s", e.Status, http.StatusText(e.Status))
}

// IsNotFound reports whether err wraps an *APIError with a 404 status code.
// Resource Read implementations use this to translate "resource deleted
// out-of-band" into a remove-from-state rather than surfacing an error.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusNotFound
	}
	return false
}

// decodeAPIError reads the response body and constructs a typed *APIError.
// Everflow returns errors as {"Error": "..."} with a capital E; we match
// that shape and fall back to the raw body if decoding fails.
func decodeAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	apiErr := &APIError{
		Status:  resp.StatusCode,
		RawBody: string(body),
	}

	if len(body) > 0 {
		var payload struct {
			Error string `json:"Error"`
		}
		if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
			apiErr.Message = payload.Error
		}
	}

	return apiErr
}
