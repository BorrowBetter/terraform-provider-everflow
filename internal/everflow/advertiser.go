// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"fmt"
	"net/http"
)

// advertisersPath is the base path for the Everflow advertiser endpoints.
// Everflow's canonical resource identifier is network_advertiser_id (int64),
// so callers embed that into the path for single-record operations.
const advertisersPath = "/v1/networks/advertisers"

// Advertiser is the subset of Everflow's advertiser resource that the
// Terraform provider models explicitly. The shape intentionally mirrors the
// API's JSON keys so round-trips through fetch-modify-put are lossless for
// these fields.
//
// Fields outside of this struct (billing, contact_address, settings, users,
// etc.) are preserved by the resource's Update method using the raw map
// variant below — not by adding more typed fields here. This keeps the
// typed surface small while still respecting Everflow's full-replacement
// PUT semantics.
type Advertiser struct {
	NetworkAdvertiserID int64  `json:"network_advertiser_id,omitempty"`
	NetworkID           int64  `json:"network_id,omitempty"`
	Name                string `json:"name"`
	AccountStatus       string `json:"account_status"`
	NetworkEmployeeID   int64  `json:"network_employee_id"`
	InternalNotes       string `json:"internal_notes,omitempty"`
	DefaultCurrencyID   string `json:"default_currency_id"`
	ReportingTimezoneID int64  `json:"reporting_timezone_id"`
	TimeCreated         int64  `json:"time_created,omitempty"`
	TimeSaved           int64  `json:"time_saved,omitempty"`
}

// CreateAdvertiserInput is the request body sent to POST
// /v1/networks/advertisers. It is a strict subset of Advertiser — only the
// fields the Create endpoint actually accepts, and none of the server-
// assigned ones.
type CreateAdvertiserInput struct {
	Name                string `json:"name"`
	AccountStatus       string `json:"account_status"`
	NetworkEmployeeID   int64  `json:"network_employee_id"`
	DefaultCurrencyID   string `json:"default_currency_id"`
	ReportingTimezoneID int64  `json:"reporting_timezone_id"`
	InternalNotes       string `json:"internal_notes,omitempty"`
}

// CreateAdvertiser issues a POST to create a new advertiser and decodes the
// response into a typed Advertiser. Any unmodeled server fields on the
// response are discarded — if the caller needs them, it should follow up
// with GetAdvertiserRaw.
func (c *Client) CreateAdvertiser(ctx context.Context, input CreateAdvertiserInput) (Advertiser, error) {
	var out Advertiser
	if err := c.DoRequest(ctx, http.MethodPost, advertisersPath, input, &out); err != nil {
		return Advertiser{}, err
	}
	return out, nil
}

// GetAdvertiser fetches a single advertiser by its network_advertiser_id and
// decodes it into a typed Advertiser. Callers that need to preserve unknown
// fields on a round trip should use GetAdvertiserRaw instead.
func (c *Client) GetAdvertiser(ctx context.Context, id int64) (Advertiser, error) {
	var out Advertiser
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", advertisersPath, id), nil, &out); err != nil {
		return Advertiser{}, err
	}
	return out, nil
}

// GetAdvertiserRaw fetches a single advertiser into a string-keyed map,
// preserving every field Everflow returns — including nested objects like
// billing, contact_address, settings, and users that the typed Advertiser
// doesn't model.
//
// This exists specifically to support the resource's fetch-modify-put
// Update strategy: the resource GETs the raw map, overlays its schema-
// managed fields, and PUTs the merged payload back. Unmodeled fields
// survive untouched across apply cycles.
func (c *Client) GetAdvertiserRaw(ctx context.Context, id int64) (map[string]any, error) {
	out := map[string]any{}
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", advertisersPath, id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAdvertiser issues a PUT to replace an advertiser. Everflow's PUT is
// a full replacement — any field the caller omits from body will be reset
// or deleted on the server — so callers should pass a map that already
// includes every field they want to preserve (typically produced by
// overlaying schema fields onto a GetAdvertiserRaw result).
func (c *Client) UpdateAdvertiser(ctx context.Context, id int64, body map[string]any) (Advertiser, error) {
	var out Advertiser
	if err := c.DoRequest(ctx, http.MethodPut, fmt.Sprintf("%s/%d", advertisersPath, id), body, &out); err != nil {
		return Advertiser{}, err
	}
	return out, nil
}
