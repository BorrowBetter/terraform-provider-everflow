// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"fmt"
	"net/http"
)

// affiliatesPath is the base path for the Everflow affiliate endpoints.
// Everflow's canonical resource identifier is network_affiliate_id (int64),
// so callers embed that into the path for single-record operations.
const affiliatesPath = "/v1/networks/affiliates"

// AffiliateBillingDetails holds the inner "details" block of a billing
// object. Flattened to a single field in the Terraform schema (day_of_month)
// but preserved as the nested JSON shape the API expects on the wire.
type AffiliateBillingDetails struct {
	DayOfMonth int64 `json:"day_of_month"`
}

// AffiliateBilling is the billing block Everflow requires on affiliate
// creation. The Terraform schema exposes billing_frequency, payment_type,
// and day_of_month (flattened from details.day_of_month).
type AffiliateBilling struct {
	BillingFrequency string                  `json:"billing_frequency"`
	PaymentType      string                  `json:"payment_type"`
	Details          AffiliateBillingDetails `json:"details"`
}

// Affiliate is the subset of Everflow's affiliate resource that the
// Terraform provider models explicitly. The shape intentionally mirrors the
// API's JSON keys so round-trips through fetch-modify-put are lossless for
// these fields.
//
// Fields outside of this struct (contact_address, users, labels, settings,
// etc.) are preserved by the resource's Update method using the raw map
// variant below — not by adding more typed fields here. This keeps the typed
// surface small while still respecting Everflow's full-replacement PUT
// semantics.
//
// Note: unlike Advertiser, Affiliate has no reporting_timezone_id field at
// the top level. Affiliate timezones live inside nested user objects, which
// are out of scope for the initial resource.
type Affiliate struct {
	NetworkAffiliateID int64            `json:"network_affiliate_id,omitempty"`
	NetworkID          int64            `json:"network_id,omitempty"`
	Name               string           `json:"name"`
	AccountStatus      string           `json:"account_status"`
	NetworkEmployeeID  int64            `json:"network_employee_id"`
	DefaultCurrencyID  string           `json:"default_currency_id"`
	InternalNotes      string           `json:"internal_notes,omitempty"`
	Billing            AffiliateBilling `json:"billing"`
	TimeCreated        int64            `json:"time_created,omitempty"`
	TimeSaved          int64            `json:"time_saved,omitempty"`
}

// CreateAffiliateInput is the request body sent to POST
// /v1/networks/affiliates. It is a strict subset of Affiliate — only the
// fields the Create endpoint actually accepts, and none of the server-
// assigned ones. The Billing field is required by the API.
type CreateAffiliateInput struct {
	Name              string           `json:"name"`
	AccountStatus     string           `json:"account_status"`
	NetworkEmployeeID int64            `json:"network_employee_id"`
	DefaultCurrencyID string           `json:"default_currency_id"`
	InternalNotes     string           `json:"internal_notes,omitempty"`
	Billing           AffiliateBilling `json:"billing"`
}

// CreateAffiliate issues a POST to create a new affiliate and decodes the
// response into a typed Affiliate. Any unmodeled server fields on the
// response are discarded — if the caller needs them, it should follow up
// with GetAffiliateRaw.
func (c *Client) CreateAffiliate(ctx context.Context, input CreateAffiliateInput) (Affiliate, error) {
	var out Affiliate
	if err := c.DoRequest(ctx, http.MethodPost, affiliatesPath, input, &out); err != nil {
		return Affiliate{}, err
	}
	return out, nil
}

// GetAffiliate fetches a single affiliate by its network_affiliate_id and
// decodes it into a typed Affiliate. Callers that need to preserve unknown
// fields on a round trip should use GetAffiliateRaw instead.
func (c *Client) GetAffiliate(ctx context.Context, id int64) (Affiliate, error) {
	var out Affiliate
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", affiliatesPath, id), nil, &out); err != nil {
		return Affiliate{}, err
	}
	return out, nil
}

// GetAffiliateRaw fetches a single affiliate into a string-keyed map,
// preserving every field Everflow returns — including nested objects like
// billing, contact_address, users, and labels that the typed Affiliate
// doesn't model.
//
// This exists specifically to support the resource's fetch-modify-put
// Update strategy: the resource GETs the raw map, overlays its schema-
// managed fields, and PUTs the merged payload back. Unmodeled fields
// survive untouched across apply cycles.
func (c *Client) GetAffiliateRaw(ctx context.Context, id int64) (map[string]any, error) {
	out := map[string]any{}
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", affiliatesPath, id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAffiliate issues a PUT to replace an affiliate. Everflow's PUT is
// a full replacement — any field the caller omits from body will be reset
// or deleted on the server — so callers should pass a map that already
// includes every field they want to preserve (typically produced by
// overlaying schema fields onto a GetAffiliateRaw result).
func (c *Client) UpdateAffiliate(ctx context.Context, id int64, body map[string]any) (Affiliate, error) {
	var out Affiliate
	if err := c.DoRequest(ctx, http.MethodPut, fmt.Sprintf("%s/%d", affiliatesPath, id), body, &out); err != nil {
		return Affiliate{}, err
	}
	return out, nil
}
