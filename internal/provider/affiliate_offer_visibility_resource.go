// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BorrowBetter/terraform-provider-everflow/internal/everflow"
)

var (
	_ resource.Resource                = &AffiliateOfferVisibilityResource{}
	_ resource.ResourceWithConfigure   = &AffiliateOfferVisibilityResource{}
	_ resource.ResourceWithImportState = &AffiliateOfferVisibilityResource{}
)

func NewAffiliateOfferVisibilityResource() resource.Resource {
	return &AffiliateOfferVisibilityResource{}
}

type AffiliateOfferVisibilityResource struct {
	client *everflow.Client
}

type AffiliateOfferVisibilityResourceModel struct {
	NetworkAffiliateID types.Int64 `tfsdk:"network_affiliate_id"`
	NetworkOfferID     types.Int64 `tfsdk:"network_offer_id"`
}

func (r *AffiliateOfferVisibilityResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_affiliate_offer_visibility"
}

func (r *AffiliateOfferVisibilityResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants an affiliate visibility on an offer via the " +
			"[Everflow Offer Visibility API](https://developers.everflow.io/docs/network/offer_visibility/).\n\n" +
			"This resource represents a single affiliate-offer visibility edge. " +
			"Its existence means the affiliate can see the offer; destroying it " +
			"revokes access. This is primarily useful for `private` offers " +
			"(see `everflow_offer.visibility`) where affiliates must be " +
			"explicitly whitelisted.\n\n" +
			"Uses the additive PATCH endpoint " +
			"(`/v1/networks/affiliates/{id}/offers/visibility`) so creating or " +
			"destroying this resource does not affect other affiliates' " +
			"visibility on the same offer.",
		Attributes: map[string]schema.Attribute{
			"network_affiliate_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID of the affiliate to grant visibility. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"network_offer_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID of the offer the affiliate should be able to see. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *AffiliateOfferVisibilityResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*everflow.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("expected *everflow.Client, got %T — this is a provider bug", req.ProviderData),
		)
		return
	}
	r.client = client
}

func (r *AffiliateOfferVisibilityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AffiliateOfferVisibilityResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	affiliateID := plan.NetworkAffiliateID.ValueInt64()
	offerID := plan.NetworkOfferID.ValueInt64()

	err := r.client.SetAffiliateOfferVisibility(ctx, affiliateID, []int64{offerID}, "visible")
	if err != nil {
		resp.Diagnostics.AddError("Failed to grant affiliate offer visibility", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read confirms the affiliate is still in the offer's visible list. If
// the offer returns 404, or the affiliate is no longer in the visible
// list (revoked out-of-band), the resource is removed from state.
func (r *AffiliateOfferVisibilityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AffiliateOfferVisibilityResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	offerID := state.NetworkOfferID.ValueInt64()
	affiliateID := state.NetworkAffiliateID.ValueInt64()

	vis, err := r.client.GetOfferVisibility(ctx, offerID)
	if err != nil {
		if everflow.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read offer visibility", err.Error())
		return
	}

	// Check if the affiliate is in the visible list.
	found := false
	for _, id := range vis.NetworkAffiliateVisibleIDs {
		if id == affiliateID {
			found = true
			break
		}
	}
	if !found {
		// Affiliate was hidden out-of-band — remove from state so
		// Terraform knows to re-create the grant.
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: both attributes use RequiresReplace, so the
// framework will never call Update — it destroys and recreates instead.
func (r *AffiliateOfferVisibilityResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

func (r *AffiliateOfferVisibilityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AffiliateOfferVisibilityResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	affiliateID := state.NetworkAffiliateID.ValueInt64()
	offerID := state.NetworkOfferID.ValueInt64()

	err := r.client.SetAffiliateOfferVisibility(ctx, affiliateID, []int64{offerID}, "hidden")
	if err != nil {
		resp.Diagnostics.AddError("Failed to revoke affiliate offer visibility", err.Error())
		return
	}
}

// ImportState accepts a composite ID in the format "affiliate_id/offer_id".
func (r *AffiliateOfferVisibilityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("expected format: affiliate_id/offer_id; got %q", req.ID),
		)
		return
	}

	affiliateID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("affiliate_id must be a base-10 integer; got %q", parts[0]),
		)
		return
	}
	offerID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("offer_id must be a base-10 integer; got %q", parts[1]),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_affiliate_id"), affiliateID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_offer_id"), offerID)...)
}
