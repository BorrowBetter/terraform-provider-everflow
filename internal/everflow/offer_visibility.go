// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"fmt"
	"net/http"
)

// offerVisibilityPath is the base path for the Everflow offer visibility
// endpoints. Used to GET the full visibility list for a single offer.
const offerVisibilityPath = "/v1/networks/offers"

// affiliateOfferVisibilityPath is the base path for the PATCH endpoint
// that manages an individual affiliate's offer visibility grants. This
// is the additive variant — it modifies only the specified affiliate's
// grants without clobbering other affiliates' visibility on the same offer.
const affiliateOfferVisibilityPath = "/v1/networks/affiliates"

// OfferVisibility is the response shape from
// GET /v1/networks/offers/{id}/visibility. It lists which affiliates
// are explicitly visible, rejected, or hidden for the given offer.
type OfferVisibility struct {
	NetworkID                   int64   `json:"network_id"`
	NetworkOfferID              int64   `json:"network_offer_id"`
	NetworkAffiliateVisibleIDs  []int64 `json:"network_affiliate_visible_ids"`
	NetworkAffiliateRejectedIDs []int64 `json:"network_affiliate_rejected_ids"`
	NetworkAffiliateHiddenIDs   []int64 `json:"network_affiliate_hidden_ids"`
}

// setAffiliateOfferVisibilityInput is the request body for
// PATCH /v1/networks/affiliates/{id}/offers/visibility.
type setAffiliateOfferVisibilityInput struct {
	NetworkOfferIDs []int64 `json:"network_offer_ids"`
	VisibilityType  string  `json:"visibility_type"`
}

// GetOfferVisibility fetches the visibility grants for a single offer.
// The returned struct contains three slices of affiliate IDs: visible,
// rejected, and hidden. Callers can check membership to determine
// whether a specific affiliate has been granted access.
func (c *Client) GetOfferVisibility(ctx context.Context, offerID int64) (OfferVisibility, error) {
	var out OfferVisibility
	path := fmt.Sprintf("%s/%d/visibility", offerVisibilityPath, offerID)
	if err := c.DoRequest(ctx, http.MethodGet, path, nil, &out); err != nil {
		return OfferVisibility{}, err
	}
	return out, nil
}

// SetAffiliateOfferVisibility sets the visibility for a single
// affiliate across one or more offers. This uses the additive PATCH
// endpoint so it does NOT clobber other affiliates' visibility.
//
// visibilityType is typically "visible" (grant access) or "hidden"
// (revoke access).
func (c *Client) SetAffiliateOfferVisibility(ctx context.Context, affiliateID int64, offerIDs []int64, visibilityType string) error {
	path := fmt.Sprintf("%s/%d/offers/visibility", affiliateOfferVisibilityPath, affiliateID)
	body := setAffiliateOfferVisibilityInput{
		NetworkOfferIDs: offerIDs,
		VisibilityType:  visibilityType,
	}
	return c.DoRequest(ctx, http.MethodPatch, path, body, nil)
}
