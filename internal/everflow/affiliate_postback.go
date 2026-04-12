// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

import (
	"context"
	"fmt"
	"net/http"
)

// pixelsPath is the base path for the Everflow pixel/postback endpoints.
// The provider names the resource "affiliate_postback" for clarity, but
// Everflow's API calls them "pixels".
const pixelsPath = "/v1/networks/pixels"

// AffiliatePostback is the subset of Everflow's pixel resource that the
// Terraform provider models explicitly. The shape mirrors the API's JSON
// keys so round-trips through fetch-modify-put are lossless for these
// fields.
//
// Fields outside this struct are preserved by the resource's Update
// method using the raw map variant — not by adding more typed fields.
type AffiliatePostback struct {
	NetworkPixelID     int64  `json:"network_pixel_id,omitempty"`
	NetworkID          int64  `json:"network_id,omitempty"`
	NetworkAffiliateID int64  `json:"network_affiliate_id"`
	DeliveryMethod     string `json:"delivery_method"`
	PixelLevel         string `json:"pixel_level"`
	PixelType          string `json:"pixel_type"`
	PixelStatus        string `json:"pixel_status"`
	PostbackURL        string `json:"postback_url"`
	DelayMS            int64  `json:"delay_ms,omitempty"`
	Description        string `json:"description,omitempty"`
	TimeCreated        int64  `json:"time_created,omitempty"`
	TimeSaved          int64  `json:"time_saved,omitempty"`
}

// CreateAffiliatePostbackInput is the request body sent to
// POST /v1/networks/pixels. It includes all required fields and
// optional ones the provider exposes. delivery_method and pixel_level
// are included here (rather than being hardcoded in the resource layer)
// so the client stays general-purpose.
type CreateAffiliatePostbackInput struct {
	NetworkAffiliateID int64  `json:"network_affiliate_id"`
	DeliveryMethod     string `json:"delivery_method"`
	PixelLevel         string `json:"pixel_level"`
	PixelType          string `json:"pixel_type"`
	PixelStatus        string `json:"pixel_status"`
	PostbackURL        string `json:"postback_url"`
	DelayMS            int64  `json:"delay_ms,omitempty"`
	Description        string `json:"description,omitempty"`
}

// CreateAffiliatePostback issues a POST to create a new pixel and
// decodes the response into a typed AffiliatePostback.
func (c *Client) CreateAffiliatePostback(ctx context.Context, input CreateAffiliatePostbackInput) (AffiliatePostback, error) {
	var out AffiliatePostback
	if err := c.DoRequest(ctx, http.MethodPost, pixelsPath, input, &out); err != nil {
		return AffiliatePostback{}, err
	}
	return out, nil
}

// GetAffiliatePostback fetches a single pixel by its network_pixel_id.
func (c *Client) GetAffiliatePostback(ctx context.Context, id int64) (AffiliatePostback, error) {
	var out AffiliatePostback
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", pixelsPath, id), nil, &out); err != nil {
		return AffiliatePostback{}, err
	}
	return out, nil
}

// GetAffiliatePostbackRaw fetches a single pixel into a string-keyed
// map, preserving every field Everflow returns. Used by the resource's
// fetch-modify-put Update strategy.
func (c *Client) GetAffiliatePostbackRaw(ctx context.Context, id int64) (map[string]any, error) {
	out := map[string]any{}
	if err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%d", pixelsPath, id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAffiliatePostback issues a PUT to replace a pixel. Everflow's
// PUT is a full replacement, so callers should pass a map produced by
// overlaying schema fields onto a GetAffiliatePostbackRaw result.
func (c *Client) UpdateAffiliatePostback(ctx context.Context, id int64, body map[string]any) (AffiliatePostback, error) {
	var out AffiliatePostback
	if err := c.DoRequest(ctx, http.MethodPut, fmt.Sprintf("%s/%d", pixelsPath, id), body, &out); err != nil {
		return AffiliatePostback{}, err
	}
	return out, nil
}
