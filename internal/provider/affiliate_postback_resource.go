// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BorrowBetter/terraform-provider-everflow/internal/everflow"
)

var (
	_ resource.Resource                = &AffiliatePostbackResource{}
	_ resource.ResourceWithConfigure   = &AffiliatePostbackResource{}
	_ resource.ResourceWithImportState = &AffiliatePostbackResource{}
)

func NewAffiliatePostbackResource() resource.Resource {
	return &AffiliatePostbackResource{}
}

type AffiliatePostbackResource struct {
	client *everflow.Client
}

type AffiliatePostbackResourceModel struct {
	NetworkPixelID     types.Int64  `tfsdk:"network_pixel_id"`
	NetworkID          types.Int64  `tfsdk:"network_id"`
	NetworkAffiliateID types.Int64  `tfsdk:"network_affiliate_id"`
	PixelType          types.String `tfsdk:"pixel_type"`
	PixelStatus        types.String `tfsdk:"pixel_status"`
	PostbackURL        types.String `tfsdk:"postback_url"`
	DelayMS            types.Int64  `tfsdk:"delay_ms"`
	Description        types.String `tfsdk:"description"`
	TimeCreated        types.Int64  `tfsdk:"time_created"`
	TimeSaved          types.Int64  `tfsdk:"time_saved"`
}

func (r *AffiliatePostbackResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_affiliate_postback"
}

func (r *AffiliatePostbackResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a global server-to-server postback URL on an " +
			"[Everflow affiliate](https://developers.everflow.io/docs/network/postbacks/) " +
			"via the Network Pixels API.\n\n" +
			"A global postback fires on **all** offer conversions for the " +
			"affiliate. `delivery_method` is hardcoded to `postback` and " +
			"`pixel_level` to `global`; offer-level postbacks are not yet " +
			"supported.\n\n" +
			"### Soft-delete semantics\n\n" +
			"Everflow has no DELETE endpoint for pixels. `terraform destroy` " +
			"instead PUTs `pixel_status = \"inactive\"` and removes the " +
			"resource from Terraform state. The record persists in Everflow " +
			"as an inactive pixel.\n\n" +
			"### Unmodeled fields\n\n" +
			"Updates are performed as fetch-modify-put to preserve any " +
			"server-side fields the schema does not expose.",
		Attributes: map[string]schema.Attribute{
			"network_pixel_id": schema.Int64Attribute{
				MarkdownDescription: "Server-assigned numeric identifier for the pixel/postback.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"network_id": schema.Int64Attribute{
				MarkdownDescription: "Identifier of the Everflow network this pixel belongs to.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"network_affiliate_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID of the affiliate this postback belongs to.",
				Required:            true,
			},
			"pixel_type": schema.StringAttribute{
				MarkdownDescription: "Type of event that triggers the postback. One of `conversion`, `post_conversion`, `cpc`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("conversion", "post_conversion", "cpc"),
				},
			},
			"pixel_status": schema.StringAttribute{
				MarkdownDescription: "Postback status. One of `active`, `inactive`. `terraform destroy` sets this to `inactive`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("active", "inactive"),
				},
			},
			"postback_url": schema.StringAttribute{
				MarkdownDescription: "The URL Everflow fires on each conversion. Supports macros like `{transaction_id}`, `{affiliate_id}`, `{sub1}`, etc.",
				Required:            true,
			},
			"delay_ms": schema.Int64Attribute{
				MarkdownDescription: "Delay in milliseconds before firing the postback (0–300000). Defaults to 0 server-side.",
				Optional:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Human-readable label for this postback.",
				Optional:            true,
			},
			"time_created": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp (seconds) the pixel was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"time_saved": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp (seconds) of the pixel's last save.",
				Computed:            true,
			},
		},
	}
}

func (r *AffiliatePostbackResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AffiliatePostbackResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AffiliatePostbackResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := everflow.CreateAffiliatePostbackInput{
		NetworkAffiliateID: plan.NetworkAffiliateID.ValueInt64(),
		DeliveryMethod:     "postback",
		PixelLevel:         "global",
		PixelType:          plan.PixelType.ValueString(),
		PixelStatus:        plan.PixelStatus.ValueString(),
		PostbackURL:        plan.PostbackURL.ValueString(),
		DelayMS:            plan.DelayMS.ValueInt64(),
		Description:        plan.Description.ValueString(),
	}

	created, err := r.client.CreateAffiliatePostback(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Everflow affiliate postback", err.Error())
		return
	}

	writeAffiliatePostbackToModel(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AffiliatePostbackResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AffiliatePostbackResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkPixelID.ValueInt64()
	got, err := r.client.GetAffiliatePostback(ctx, id)
	if err != nil {
		if everflow.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read Everflow affiliate postback", err.Error())
		return
	}

	writeAffiliatePostbackToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AffiliatePostbackResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AffiliatePostbackResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state AffiliatePostbackResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkPixelID.ValueInt64()
	merged, err := r.fetchAndOverlayPostback(ctx, id, plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch existing Everflow postback for update", err.Error())
		return
	}

	updated, err := r.client.UpdateAffiliatePostback(ctx, id, merged)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update Everflow affiliate postback", err.Error())
		return
	}

	writeAffiliatePostbackToModel(updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete performs a soft delete: fetch the raw record, overlay
// pixel_status = "inactive" (and ONLY that field), PUT the merged body.
func (r *AffiliatePostbackResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AffiliatePostbackResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkPixelID.ValueInt64()

	raw, err := r.client.GetAffiliatePostbackRaw(ctx, id)
	if err != nil {
		if everflow.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to fetch Everflow postback for soft-delete", err.Error())
		return
	}
	raw["pixel_status"] = "inactive"

	if _, err := r.client.UpdateAffiliatePostback(ctx, id, raw); err != nil {
		resp.Diagnostics.AddError("Failed to soft-delete Everflow affiliate postback", err.Error())
		return
	}
}

func (r *AffiliatePostbackResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("network_pixel_id must be a base-10 integer; got %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_pixel_id"), id)...)
}

// fetchAndOverlayPostback GETs the raw pixel from Everflow and overlays
// the schema-managed fields from plan. Unmodeled fields survive.
func (r *AffiliatePostbackResource) fetchAndOverlayPostback(ctx context.Context, id int64, plan AffiliatePostbackResourceModel) (map[string]any, error) {
	raw, err := r.client.GetAffiliatePostbackRaw(ctx, id)
	if err != nil {
		return nil, err
	}

	raw["network_affiliate_id"] = plan.NetworkAffiliateID.ValueInt64()
	raw["delivery_method"] = "postback"
	raw["pixel_level"] = "global"
	raw["pixel_type"] = plan.PixelType.ValueString()
	raw["pixel_status"] = plan.PixelStatus.ValueString()
	raw["postback_url"] = plan.PostbackURL.ValueString()
	raw["delay_ms"] = plan.DelayMS.ValueInt64()

	// B1: description is optional free-text — write "" on null to clear.
	raw["description"] = plan.Description.ValueString()

	return raw, nil
}

// writeAffiliatePostbackToModel copies server values into a Terraform
// model in place.
func writeAffiliatePostbackToModel(src everflow.AffiliatePostback, dst *AffiliatePostbackResourceModel) {
	dst.NetworkPixelID = types.Int64Value(src.NetworkPixelID)
	dst.NetworkID = types.Int64Value(src.NetworkID)
	dst.NetworkAffiliateID = types.Int64Value(src.NetworkAffiliateID)
	dst.PixelType = types.StringValue(src.PixelType)
	dst.PixelStatus = types.StringValue(src.PixelStatus)
	dst.PostbackURL = types.StringValue(src.PostbackURL)
	dst.TimeCreated = types.Int64Value(src.TimeCreated)
	dst.TimeSaved = types.Int64Value(src.TimeSaved)

	// delay_ms: 0 is the server default — normalize to null for
	// drift-free round trip when the user doesn't set it.
	if src.DelayMS == 0 {
		dst.DelayMS = types.Int64Null()
	} else {
		dst.DelayMS = types.Int64Value(src.DelayMS)
	}

	// B1: description uses empty → null normalization.
	if src.Description == "" {
		dst.Description = types.StringNull()
	} else {
		dst.Description = types.StringValue(src.Description)
	}
}
