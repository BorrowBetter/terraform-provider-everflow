// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// offersPath is the base path for the Everflow offer endpoints. Everflow's
// canonical resource identifier is network_offer_id (int64), so callers
// embed that into the path for single-record operations.
const offersPath = "/v1/networks/offers"

// PayoutRevenueEntry is a single entry in an offer's payout_revenue array.
//
// Everflow requires at least one entry and exactly one entry flagged
// is_default=true. Several scalar fields are conditionally required based
// on the entry's payout_type / revenue_type (e.g. payout_percentage is
// only meaningful for cps/cpa_cps/prv payouts). We intentionally leave
// the conditional semantics to the server: the struct tags use omitempty
// so zero-valued optional fields drop out of the wire representation
// rather than being sent as 0 / "".
type PayoutRevenueEntry struct {
	EntryName         string  `json:"entry_name,omitempty"`
	PayoutType        string  `json:"payout_type"`
	PayoutAmount      float64 `json:"payout_amount,omitempty"`
	PayoutPercentage  int64   `json:"payout_percentage,omitempty"`
	RevenueType       string  `json:"revenue_type"`
	RevenueAmount     float64 `json:"revenue_amount,omitempty"`
	RevenuePercentage int64   `json:"revenue_percentage,omitempty"`
	IsDefault         bool    `json:"is_default"`
	IsPrivate         bool    `json:"is_private"`
}

// Offer is the subset of Everflow's offer resource that the Terraform
// provider models explicitly. The shape intentionally mirrors the API's
// JSON keys so round-trips through fetch-modify-put are lossless for
// these fields.
//
// Fields outside of this struct (ruleset, traffic_filters, creatives,
// labels, visibility, category, conversion caps, etc.) are preserved by
// the resource's Update method using the raw map variant below — not by
// adding more typed fields here. This keeps the typed surface small
// while still respecting Everflow's full-replacement PUT semantics.
//
// payout_revenue IS schema-visible, so the user owns it as Terraform
// state. UI edits to payouts get clobbered on next apply — this is the
// documented contract and matches how any other schema-managed attribute
// behaves.
type Offer struct {
	NetworkOfferID          int64                `json:"network_offer_id,omitempty"`
	NetworkID               int64                `json:"network_id,omitempty"`
	Name                    string               `json:"name"`
	NetworkAdvertiserID     int64                `json:"network_advertiser_id"`
	DestinationURL          string               `json:"destination_url"`
	OfferStatus             string               `json:"offer_status"`
	CurrencyID              string               `json:"currency_id"`
	ConversionMethod        string               `json:"conversion_method"`
	NetworkTrackingDomainID int64                `json:"network_tracking_domain_id"`
	InternalNotes           string               `json:"internal_notes,omitempty"`
	PayoutRevenue           []PayoutRevenueEntry `json:"payout_revenue,omitempty"`
	TimeCreated             int64                `json:"time_created,omitempty"`
	TimeSaved               int64                `json:"time_saved,omitempty"`
}

// UnmarshalJSON implements custom decoding so Offer can absorb both the
// top-level `payout_revenue` shape (used by POST/PUT request bodies) and
// the nested `relationship.payout_revenue.entries` shape that Everflow's
// GET responses return.
//
// Everflow's API is asymmetric: when you POST or PUT an offer, the request
// body takes `payout_revenue` at the top level as a plain array. When you
// GET the same offer, the response wraps it under a `relationship` object
// as `relationship.payout_revenue.entries`. A naive decoder keyed on the
// top-level field alone silently drops the payouts on Read, which in turn
// breaks `terraform import` for offers because the imported state has an
// empty `payout_revenue` slice.
//
// The two shapes are treated as a union: if the top-level key is present,
// it wins; otherwise the decoder falls back to the nested entries. This
// keeps the client layer honest about both the request and response
// contracts Everflow actually uses.
func (o *Offer) UnmarshalJSON(data []byte) error {
	// Alias prevents the decoder from recursing into this method.
	type alias Offer
	aux := struct {
		*alias
		Relationship *struct {
			PayoutRevenue *struct {
				Entries []PayoutRevenueEntry `json:"entries"`
			} `json:"payout_revenue"`
		} `json:"relationship"`
	}{alias: (*alias)(o)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(o.PayoutRevenue) == 0 && aux.Relationship != nil && aux.Relationship.PayoutRevenue != nil {
		o.PayoutRevenue = aux.Relationship.PayoutRevenue.Entries
	}
	return nil
}

// CreateOfferInput is the request body sent to POST /v1/networks/offers.
// It is a strict subset of Offer — only the fields the Create endpoint
// actually accepts, and none of the server-assigned ones. PayoutRevenue
// is NOT tagged omitempty here: the API requires at least one entry.
type CreateOfferInput struct {
	Name                    string               `json:"name"`
	NetworkAdvertiserID     int64                `json:"network_advertiser_id"`
	DestinationURL          string               `json:"destination_url"`
	OfferStatus             string               `json:"offer_status"`
	CurrencyID              string               `json:"currency_id"`
	ConversionMethod        string               `json:"conversion_method"`
	NetworkTrackingDomainID int64                `json:"network_tracking_domain_id"`
	InternalNotes           string               `json:"internal_notes,omitempty"`
	PayoutRevenue           []PayoutRevenueEntry `json:"payout_revenue"`
}

// CreateOffer issues a POST to create a new offer and decodes the response
// into a typed Offer. Any unmodeled server fields on the response are
// discarded — if the caller needs them, it should follow up with
// GetOfferRaw.
func (c *Client) CreateOffer(ctx context.Context, input CreateOfferInput) (Offer, error) {
	var out Offer
	if err := c.DoRequest(ctx, http.MethodPost, offersPath, input, &out); err != nil {
		return Offer{}, err
	}
	return out, nil
}

// GetOffer fetches a single offer by its network_offer_id and decodes it
// into a typed Offer. Callers that need to preserve unknown fields on a
// round trip should use GetOfferRaw instead.
func (c *Client) GetOffer(ctx context.Context, id int64) (Offer, error) {
	var out Offer
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", offersPath, id), nil, &out); err != nil {
		return Offer{}, err
	}
	return out, nil
}

// GetOfferRaw fetches a single offer into a string-keyed map, preserving
// every field Everflow returns — including nested objects like ruleset,
// traffic_filters, creatives, labels, visibility, category, and caps that
// the typed Offer doesn't model.
//
// This exists specifically to support the resource's fetch-modify-put
// Update strategy: the resource GETs the raw map, overlays its schema-
// managed fields, and PUTs the merged payload back. Unmodeled fields
// survive untouched across apply cycles.
func (c *Client) GetOfferRaw(ctx context.Context, id int64) (map[string]any, error) {
	out := map[string]any{}
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", offersPath, id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateOffer issues a PUT to replace an offer. Everflow's PUT is a full
// replacement — any field the caller omits from body will be reset or
// deleted on the server — so callers should pass a map that already
// includes every field they want to preserve (typically produced by
// overlaying schema fields onto a GetOfferRaw result).
func (c *Client) UpdateOffer(ctx context.Context, id int64, body map[string]any) (Offer, error) {
	var out Offer
	if err := c.DoRequest(ctx, http.MethodPut, fmt.Sprintf("%s/%d", offersPath, id), body, &out); err != nil {
		return Offer{}, err
	}
	return out, nil
}
