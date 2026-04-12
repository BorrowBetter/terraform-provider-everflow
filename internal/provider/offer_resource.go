// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BorrowBetter/terraform-provider-everflow/internal/everflow"
)

// Ensure OfferResource satisfies the resource interfaces the framework
// requires. Asserting here catches interface drift at compile time rather
// than at provider startup.
var (
	_ resource.Resource                = &OfferResource{}
	_ resource.ResourceWithConfigure   = &OfferResource{}
	_ resource.ResourceWithImportState = &OfferResource{}
)

// NewOfferResource is the factory registered in provider.Resources().
func NewOfferResource() resource.Resource {
	return &OfferResource{}
}

// OfferResource implements the everflow_offer resource. It holds a
// reference to the shared Everflow client populated during Configure.
type OfferResource struct {
	client *everflow.Client
}

// OfferResourceModel maps the Terraform schema to Go types for plan,
// state, and config manipulation. Every attribute declared in Schema must
// appear here with a matching tfsdk tag.
//
// PayoutRevenue uses a Go slice rather than types.List so the framework's
// reflection marshaler handles conversion automatically — no ElementsAs
// ceremony in Create/Update/Delete.
type OfferResourceModel struct {
	NetworkOfferID          types.Int64          `tfsdk:"network_offer_id"`
	NetworkID               types.Int64          `tfsdk:"network_id"`
	TimeCreated             types.Int64          `tfsdk:"time_created"`
	TimeSaved               types.Int64          `tfsdk:"time_saved"`
	Name                    types.String         `tfsdk:"name"`
	NetworkAdvertiserID     types.Int64          `tfsdk:"network_advertiser_id"`
	DestinationURL          types.String         `tfsdk:"destination_url"`
	OfferStatus             types.String         `tfsdk:"offer_status"`
	CurrencyID              types.String         `tfsdk:"currency_id"`
	ConversionMethod        types.String         `tfsdk:"conversion_method"`
	NetworkTrackingDomainID types.Int64          `tfsdk:"network_tracking_domain_id"`
	Visibility              types.String         `tfsdk:"visibility"`
	InternalNotes           types.String         `tfsdk:"internal_notes"`
	PayoutRevenue           []PayoutRevenueModel `tfsdk:"payout_revenue"`
}

// PayoutRevenueModel is the tfsdk view of a single payout_revenue block.
// Optional scalars use types.Float64/Int64/String so null / zero can be
// distinguished in the plan. The required bool flags use the native Go
// type because the framework always supplies a value at plan time.
type PayoutRevenueModel struct {
	EntryName         types.String  `tfsdk:"entry_name"`
	PayoutType        types.String  `tfsdk:"payout_type"`
	PayoutAmount      types.Float64 `tfsdk:"payout_amount"`
	PayoutPercentage  types.Int64   `tfsdk:"payout_percentage"`
	RevenueType       types.String  `tfsdk:"revenue_type"`
	RevenueAmount     types.Float64 `tfsdk:"revenue_amount"`
	RevenuePercentage types.Int64   `tfsdk:"revenue_percentage"`
	IsDefault         types.Bool    `tfsdk:"is_default"`
	IsPrivate         types.Bool    `tfsdk:"is_private"`
}

// Metadata sets the fully-qualified resource type name (e.g. everflow_offer).
func (r *OfferResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_offer"
}

// Schema declares the attributes exposed by everflow_offer. Only the
// fields needed to create/update an offer at the minimum-viable level
// (the 8 POST-required fields plus internal_notes plus payout_revenue)
// are surfaced; unmodeled server-side fields (ruleset, traffic_filters,
// creatives, labels, visibility, category, caps) are preserved across
// apply cycles by the Update method's fetch-modify-put strategy.
func (r *OfferResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an [Everflow offer](https://developers.everflow.io/docs/network/offers/) via the Network API.\n\n" +
			"### Soft-delete semantics\n\n" +
			"Everflow has no DELETE endpoint for offers. `terraform destroy` " +
			"instead PUTs `offer_status = \"deleted\"` and removes the resource " +
			"from Terraform state. The record persists in Everflow as a deleted " +
			"offer. To fully remove a resource from state without marking it " +
			"deleted server-side, use `terraform state rm` before " +
			"`terraform destroy`.\n\n" +
			"### Unmodeled fields\n\n" +
			"Everflow's PUT endpoint is a full replacement — any field not " +
			"included in the request body is reset to defaults. To avoid " +
			"clobbering nested objects the schema does not expose (e.g. " +
			"`ruleset`, `traffic_filters`, `creatives`, `labels`, " +
			"`category`, conversion caps), updates are performed as " +
			"fetch-modify-put: the existing record is GETed, the schema-managed " +
			"fields are overlaid, and the merged payload is PUT back. " +
			"Out-of-band edits to unmodeled fields are preserved across apply " +
			"cycles.\n\n" +
			"### Payout / revenue\n\n" +
			"The `payout_revenue` block is schema-managed: the HCL is the source " +
			"of truth. At least one entry is required and exactly one must have " +
			"`is_default = true`. UI edits to payouts are clobbered on the next " +
			"apply, same as any other schema-managed attribute.",
		Attributes: map[string]schema.Attribute{
			"network_offer_id": schema.Int64Attribute{
				MarkdownDescription: "Server-assigned numeric identifier. This is the canonical primary key used by every other Everflow API that references an offer.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"network_id": schema.Int64Attribute{
				MarkdownDescription: "Identifier of the Everflow network this offer belongs to. Computed at create time.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"time_created": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp (seconds) the offer was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"time_saved": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp (seconds) of the offer's last save. Bumped by every PUT the resource issues, so plans that produce an Update will show this as `(known after apply)`.",
				Computed:            true,
				// Intentionally no UseStateForUnknown: every Update bumps
				// time_saved server-side, so the planned value genuinely
				// is unknown until after apply.
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name of the offer. Shown in the Everflow UI and on affiliate-facing reports.",
				Required:            true,
			},
			"network_advertiser_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID of the advertiser that owns this offer. Typically sourced from an `everflow_advertiser` resource via `everflow_advertiser.<name>.network_advertiser_id`.",
				Required:            true,
			},
			"destination_url": schema.StringAttribute{
				MarkdownDescription: "The landing page the offer sends traffic to. Everflow substitutes tracking macros into this URL at click time.",
				Required:            true,
			},
			"offer_status": schema.StringAttribute{
				MarkdownDescription: "Offer status. One of `active`, `paused`, `pending`, `deleted`. `terraform destroy` sets this to `deleted` — see the resource description for soft-delete semantics.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("active", "paused", "pending", "deleted"),
				},
			},
			"currency_id": schema.StringAttribute{
				MarkdownDescription: "ISO 4217 currency code used for payouts and revenue on this offer (e.g. `USD`, `EUR`).",
				Required:            true,
			},
			"conversion_method": schema.StringAttribute{
				MarkdownDescription: "How conversions are tracked. One of `server_postback`, `cookie_based`, `http_image_pixel`, `https_image_pixel`, `http_iframe_pixel`, `https_iframe_pixel`, `javascript`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						"server_postback",
						"cookie_based",
						"http_image_pixel",
						"https_image_pixel",
						"http_iframe_pixel",
						"https_iframe_pixel",
						"javascript",
					),
				},
			},
			"network_tracking_domain_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID of the Everflow tracking domain used for click/conversion URLs on this offer.",
				Required:            true,
			},
			"visibility": schema.StringAttribute{
				MarkdownDescription: "Offer visibility. One of `public` (anyone can run), `require_approval` (affiliates must apply), `private` (hidden unless explicitly whitelisted via `everflow_affiliate_offer_visibility`). Defaults to `public` server-side when omitted on create.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("public", "require_approval", "private"),
				},
			},
			"internal_notes": schema.StringAttribute{
				MarkdownDescription: "Free-form notes visible only to network employees. A good place for Terraform-managed markers (e.g. `Managed by Terraform — do not edit in UI`).",
				Optional:            true,
			},
		},
		Blocks: map[string]schema.Block{
			"payout_revenue": schema.ListNestedBlock{
				MarkdownDescription: "Payout and revenue rules for this offer. At least one entry is required; exactly one must have `is_default = true`. This block is schema-managed — UI edits to payouts are clobbered on the next apply.",
				Validators: []validator.List{
					// IsRequired catches a completely omitted block
					// (null); SizeAtLeast catches an explicitly empty
					// configured list. Both are required because the
					// API rejects either shape.
					listvalidator.IsRequired(),
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"entry_name": schema.StringAttribute{
							MarkdownDescription: "Human-readable name for this payout/revenue entry. Defaults to `Base` server-side when omitted.",
							Optional:            true,
						},
						"payout_type": schema.StringAttribute{
							MarkdownDescription: "Payout model. One of `cpc`, `cpa`, `cpm`, `cps`, `cpa_cps`, `prv`, or `null_value`. Use `null_value` for secondary entries that track revenue-only events without paying out (a common shape for multi-entry offers).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("cpc", "cpa", "cpm", "cps", "cpa_cps", "prv", "null_value"),
							},
						},
						"payout_amount": schema.Float64Attribute{
							MarkdownDescription: "Fixed payout amount in the offer's `currency_id`. Not required when `payout_type` is `cps` or `prv`.",
							Optional:            true,
						},
						"payout_percentage": schema.Int64Attribute{
							MarkdownDescription: "Payout percentage (0-100). Only meaningful when `payout_type` is `cps`, `cpa_cps`, or `prv`.",
							Optional:            true,
						},
						"revenue_type": schema.StringAttribute{
							MarkdownDescription: "Revenue model. One of `rpc`, `rpa`, `rpm`, `rps`, `rpa_rps`, or `null_value`. Use `null_value` for entries that track payout-only events without revenue, analogous to `payout_type = null_value`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("rpc", "rpa", "rpm", "rps", "rpa_rps", "null_value"),
							},
						},
						"revenue_amount": schema.Float64Attribute{
							MarkdownDescription: "Fixed revenue amount in the offer's `currency_id`. Not required when `revenue_type` is `rps`.",
							Optional:            true,
						},
						"revenue_percentage": schema.Int64Attribute{
							MarkdownDescription: "Revenue percentage (0-100). Only meaningful when `revenue_type` is `rps` or `rpa_rps`.",
							Optional:            true,
						},
						"is_default": schema.BoolAttribute{
							MarkdownDescription: "Whether this entry is the default payout/revenue for the offer. Exactly one entry must be `true`.",
							Required:            true,
						},
						"is_private": schema.BoolAttribute{
							MarkdownDescription: "Whether this entry is private (shown only to affiliates it's explicitly granted to).",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// Configure wires the shared *everflow.Client into the resource. Called
// once per resource instance during provider startup.
func (r *OfferResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		// Configure is called once before provider config is loaded. This
		// path is a normal no-op; the framework will call Configure again.
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

// Create issues a POST to Everflow and writes the decoded response (plus
// all computed fields) back to Terraform state.
func (r *OfferResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OfferResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := everflow.CreateOfferInput{
		Name:                    plan.Name.ValueString(),
		NetworkAdvertiserID:     plan.NetworkAdvertiserID.ValueInt64(),
		DestinationURL:          plan.DestinationURL.ValueString(),
		OfferStatus:             plan.OfferStatus.ValueString(),
		CurrencyID:              plan.CurrencyID.ValueString(),
		ConversionMethod:        plan.ConversionMethod.ValueString(),
		NetworkTrackingDomainID: plan.NetworkTrackingDomainID.ValueInt64(),
		Visibility:              plan.Visibility.ValueString(),
		InternalNotes:           plan.InternalNotes.ValueString(),
		PayoutRevenue:           payoutRevenueModelsToClient(plan.PayoutRevenue),
	}

	created, err := r.client.CreateOffer(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Everflow offer", err.Error())
		return
	}

	writeOfferToModel(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the local state from Everflow. A 404 is interpreted as
// "the offer was deleted out-of-band"; in that case we remove the resource
// from state instead of surfacing an error, which is the framework-
// idiomatic behavior.
func (r *OfferResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OfferResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkOfferID.ValueInt64()
	got, err := r.client.GetOffer(ctx, id)
	if err != nil {
		if everflow.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read Everflow offer", err.Error())
		return
	}

	writeOfferToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements the fetch-modify-put strategy: GET the raw map, overlay
// the schema-managed fields with the plan values, PUT the merged body. This
// keeps unmodeled fields (ruleset, traffic_filters, creatives, labels, ...)
// intact across Terraform-managed updates while still letting the schema-
// visible payout_revenue block overwrite the server's array entirely.
func (r *OfferResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OfferResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state OfferResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkOfferID.ValueInt64()
	merged, err := r.fetchAndOverlay(ctx, id, plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch existing Everflow offer for update", err.Error())
		return
	}

	updated, err := r.client.UpdateOffer(ctx, id, merged)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update Everflow offer", err.Error())
		return
	}

	writeOfferToModel(updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete performs a soft delete: fetch the raw record, overlay
// offer_status = "deleted" (and ONLY that field), PUT the merged body,
// and let the framework remove the resource from state. The record
// persists in Everflow as a deleted offer.
//
// Unlike Update, Delete does not reconcile other schema-managed fields
// back to their state values. If the user made out-of-band changes to,
// say, `name` before running `terraform destroy`, those edits are
// preserved — the only field the destroy PUT touches is `offer_status`.
// This mirrors the intent of a destroy operation ("I'm done with this
// resource, stop touching it") rather than a full rewrite.
func (r *OfferResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OfferResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkOfferID.ValueInt64()

	raw, err := r.client.GetOfferRaw(ctx, id)
	if err != nil {
		if everflow.IsNotFound(err) {
			// Already gone server-side — nothing to mark deleted.
			return
		}
		resp.Diagnostics.AddError("Failed to fetch Everflow offer for soft-delete", err.Error())
		return
	}
	raw["offer_status"] = "deleted"

	if _, err := r.client.UpdateOffer(ctx, id, raw); err != nil {
		resp.Diagnostics.AddError("Failed to soft-delete Everflow offer", err.Error())
		return
	}
}

// ImportState lets `terraform import` accept a numeric network_offer_id
// on the command line. The framework's default passthrough would write a
// string into the ID field; we parse it to int64 first so the value lands
// in the correct typed attribute.
func (r *OfferResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("network_offer_id must be a base-10 integer; got %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_offer_id"), id)...)
}

// fetchAndOverlay is the shared plumbing for Update. It GETs the raw offer
// from Everflow and overlays the schema-managed fields from plan onto the
// raw map. Fields outside the typed schema are preserved untouched so the
// resulting map can be PUT back without clobbering unmodeled server state.
//
// payout_revenue is special: the block is schema-visible, so we *overwrite*
// the server's array entirely with the planned value. Any UI-side edits to
// payouts are intentionally clobbered here — Terraform is the source of
// truth for payouts by design.
func (r *OfferResource) fetchAndOverlay(ctx context.Context, id int64, plan OfferResourceModel) (map[string]any, error) {
	raw, err := r.client.GetOfferRaw(ctx, id)
	if err != nil {
		return nil, err
	}

	raw["name"] = plan.Name.ValueString()
	raw["network_advertiser_id"] = plan.NetworkAdvertiserID.ValueInt64()
	raw["destination_url"] = plan.DestinationURL.ValueString()
	raw["offer_status"] = plan.OfferStatus.ValueString()
	raw["currency_id"] = plan.CurrencyID.ValueString()
	raw["conversion_method"] = plan.ConversionMethod.ValueString()
	raw["network_tracking_domain_id"] = plan.NetworkTrackingDomainID.ValueInt64()

	// Visibility: Optional+Computed with UseStateForUnknown, so the plan
	// always carries a value (either user-set or carried forward from
	// state). Unlike internal_notes, this is an enum — "" is never a
	// valid value, and the server always returns a non-empty string.
	raw["visibility"] = plan.Visibility.ValueString()

	// Optional field: write an explicit empty string when the plan is
	// null. Everflow's PUT is full replacement, so *omitting* the key
	// from the body would leave a stale value behind on the server; we
	// need to send "" to actually clear it. Read-side normalization
	// (writeOfferToModel) maps "" back to null so the null round-trip
	// is drift-free.
	raw["internal_notes"] = plan.InternalNotes.ValueString()

	// Overwrite the server's payout_revenue with the planned value. The
	// block is schema-managed; the user's HCL is the source of truth.
	//
	// Everflow's GET response nests the authoritative payout_revenue
	// array under `relationship.payout_revenue.entries`. We strip that
	// nested copy before PUT so the server only sees our top-level
	// overlay — otherwise a PUT body could carry two conflicting shapes
	// and the server's behavior on which one wins would be undefined.
	if rel, ok := raw["relationship"].(map[string]any); ok {
		delete(rel, "payout_revenue")
	}
	raw["payout_revenue"] = payoutRevenueModelsToMap(plan.PayoutRevenue)

	return raw, nil
}

// writeOfferToModel copies server values into a Terraform model in place.
// It's used by Create, Read, and Update — any computed field handled by
// the framework's UseStateForUnknown plan modifier needs to be populated
// here or the framework will panic with "unknown after apply".
func writeOfferToModel(src everflow.Offer, dst *OfferResourceModel) {
	dst.NetworkOfferID = types.Int64Value(src.NetworkOfferID)
	dst.NetworkID = types.Int64Value(src.NetworkID)
	dst.TimeCreated = types.Int64Value(src.TimeCreated)
	dst.TimeSaved = types.Int64Value(src.TimeSaved)
	dst.Name = types.StringValue(src.Name)
	dst.NetworkAdvertiserID = types.Int64Value(src.NetworkAdvertiserID)
	dst.DestinationURL = types.StringValue(src.DestinationURL)
	dst.OfferStatus = types.StringValue(src.OfferStatus)
	dst.CurrencyID = types.StringValue(src.CurrencyID)
	dst.ConversionMethod = types.StringValue(src.ConversionMethod)
	dst.NetworkTrackingDomainID = types.Int64Value(src.NetworkTrackingDomainID)
	dst.Visibility = types.StringValue(src.Visibility)
	if src.InternalNotes == "" {
		dst.InternalNotes = types.StringNull()
	} else {
		dst.InternalNotes = types.StringValue(src.InternalNotes)
	}
	dst.PayoutRevenue = payoutRevenueClientToModels(src.PayoutRevenue)
}

// payoutRevenueModelsToClient converts the tfsdk slice into the typed
// client struct used by CreateOffer. Null optional fields decode to the
// struct's zero value, which the client layer drops via omitempty.
func payoutRevenueModelsToClient(in []PayoutRevenueModel) []everflow.PayoutRevenueEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]everflow.PayoutRevenueEntry, len(in))
	for i, e := range in {
		out[i] = everflow.PayoutRevenueEntry{
			EntryName:         e.EntryName.ValueString(),
			PayoutType:        e.PayoutType.ValueString(),
			PayoutAmount:      e.PayoutAmount.ValueFloat64(),
			PayoutPercentage:  e.PayoutPercentage.ValueInt64(),
			RevenueType:       e.RevenueType.ValueString(),
			RevenueAmount:     e.RevenueAmount.ValueFloat64(),
			RevenuePercentage: e.RevenuePercentage.ValueInt64(),
			IsDefault:         e.IsDefault.ValueBool(),
			IsPrivate:         e.IsPrivate.ValueBool(),
		}
	}
	return out
}

// payoutRevenueModelsToMap converts the tfsdk slice into the raw
// []map[string]any shape used by Update's fetch-modify-put. Null optional
// scalars are omitted from the per-entry map so the server only receives
// the fields the user actually set.
func payoutRevenueModelsToMap(in []PayoutRevenueModel) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, len(in))
	for i, e := range in {
		entry := map[string]any{
			"payout_type":  e.PayoutType.ValueString(),
			"revenue_type": e.RevenueType.ValueString(),
			"is_default":   e.IsDefault.ValueBool(),
			"is_private":   e.IsPrivate.ValueBool(),
		}
		if !e.EntryName.IsNull() && !e.EntryName.IsUnknown() {
			entry["entry_name"] = e.EntryName.ValueString()
		}
		if !e.PayoutAmount.IsNull() && !e.PayoutAmount.IsUnknown() {
			entry["payout_amount"] = e.PayoutAmount.ValueFloat64()
		}
		if !e.PayoutPercentage.IsNull() && !e.PayoutPercentage.IsUnknown() {
			entry["payout_percentage"] = e.PayoutPercentage.ValueInt64()
		}
		if !e.RevenueAmount.IsNull() && !e.RevenueAmount.IsUnknown() {
			entry["revenue_amount"] = e.RevenueAmount.ValueFloat64()
		}
		if !e.RevenuePercentage.IsNull() && !e.RevenuePercentage.IsUnknown() {
			entry["revenue_percentage"] = e.RevenuePercentage.ValueInt64()
		}
		out[i] = entry
	}
	return out
}

// payoutRevenueClientToModels converts the typed client slice into the
// tfsdk slice used by state. Zero-valued optional fields come back as
// null so the plan→state round trip is drift-free.
func payoutRevenueClientToModels(in []everflow.PayoutRevenueEntry) []PayoutRevenueModel {
	if len(in) == 0 {
		return nil
	}
	out := make([]PayoutRevenueModel, len(in))
	for i, e := range in {
		m := PayoutRevenueModel{
			PayoutType:  types.StringValue(e.PayoutType),
			RevenueType: types.StringValue(e.RevenueType),
			IsDefault:   types.BoolValue(e.IsDefault),
			IsPrivate:   types.BoolValue(e.IsPrivate),
		}
		if e.EntryName == "" {
			m.EntryName = types.StringNull()
		} else {
			m.EntryName = types.StringValue(e.EntryName)
		}
		if e.PayoutAmount == 0 {
			m.PayoutAmount = types.Float64Null()
		} else {
			m.PayoutAmount = types.Float64Value(e.PayoutAmount)
		}
		if e.PayoutPercentage == 0 {
			m.PayoutPercentage = types.Int64Null()
		} else {
			m.PayoutPercentage = types.Int64Value(e.PayoutPercentage)
		}
		if e.RevenueAmount == 0 {
			m.RevenueAmount = types.Float64Null()
		} else {
			m.RevenueAmount = types.Float64Value(e.RevenueAmount)
		}
		if e.RevenuePercentage == 0 {
			m.RevenuePercentage = types.Int64Null()
		} else {
			m.RevenuePercentage = types.Int64Value(e.RevenuePercentage)
		}
		out[i] = m
	}
	return out
}
