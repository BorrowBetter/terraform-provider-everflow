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

// Ensure AdvertiserResource satisfies the resource interfaces the framework
// requires. Asserting here catches interface drift at compile time rather
// than at provider startup.
var (
	_ resource.Resource                = &AdvertiserResource{}
	_ resource.ResourceWithConfigure   = &AdvertiserResource{}
	_ resource.ResourceWithImportState = &AdvertiserResource{}
)

// NewAdvertiserResource is the factory registered in provider.Resources().
func NewAdvertiserResource() resource.Resource {
	return &AdvertiserResource{}
}

// AdvertiserResource implements the everflow_advertiser resource. It holds a
// reference to the shared Everflow client populated during Configure.
type AdvertiserResource struct {
	client *everflow.Client
}

// AdvertiserResourceModel maps the Terraform schema to Go types for plan,
// state, and config manipulation. Every attribute declared in Schema must
// appear here with a matching tfsdk tag.
type AdvertiserResourceModel struct {
	NetworkAdvertiserID types.Int64  `tfsdk:"network_advertiser_id"`
	NetworkID           types.Int64  `tfsdk:"network_id"`
	TimeCreated         types.Int64  `tfsdk:"time_created"`
	TimeSaved           types.Int64  `tfsdk:"time_saved"`
	Name                types.String `tfsdk:"name"`
	AccountStatus       types.String `tfsdk:"account_status"`
	NetworkEmployeeID   types.Int64  `tfsdk:"network_employee_id"`
	DefaultCurrencyID   types.String `tfsdk:"default_currency_id"`
	ReportingTimezoneID types.Int64  `tfsdk:"reporting_timezone_id"`
	InternalNotes       types.String `tfsdk:"internal_notes"`
}

// Metadata sets the fully-qualified resource type name (e.g. everflow_advertiser).
func (r *AdvertiserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_advertiser"
}

// Schema declares the attributes exposed by everflow_advertiser. Only the
// fields needed to create/update an advertiser at the minimum-viable level
// are surfaced; unmodeled server-side fields (billing, contact_address,
// settings, users) are preserved across apply cycles by the Update method's
// fetch-modify-put strategy.
func (r *AdvertiserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an [Everflow advertiser](https://developers.everflow.io/docs/network/advertisers/) via the Network API.\n\n" +
			"### Soft-delete semantics\n\n" +
			"Everflow has no DELETE endpoint for advertisers. `terraform destroy` " +
			"instead PUTs `account_status = \"inactive\"` and removes the resource " +
			"from Terraform state. The record persists in Everflow as an inactive " +
			"advertiser. To fully remove a resource from state without deactivating " +
			"it, use `terraform state rm` before `terraform destroy`.\n\n" +
			"### Unmodeled fields\n\n" +
			"Everflow's PUT endpoint is a full replacement — any field not included " +
			"in the request body is reset to defaults. To avoid clobbering nested " +
			"objects the schema does not expose (e.g. `billing`, `contact_address`, " +
			"`settings`, `users`), updates are performed as fetch-modify-put: the " +
			"existing record is GETed, the schema-managed fields are overlaid, and " +
			"the merged payload is PUT back. Out-of-band edits to unmodeled fields " +
			"are preserved across apply cycles.",
		Attributes: map[string]schema.Attribute{
			"network_advertiser_id": schema.Int64Attribute{
				MarkdownDescription: "Server-assigned numeric identifier. This is the canonical primary key used by every other Everflow API that references an advertiser.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"network_id": schema.Int64Attribute{
				MarkdownDescription: "Identifier of the Everflow network this advertiser belongs to. Computed at create time.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"time_created": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp (seconds) the advertiser was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"time_saved": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp (seconds) of the advertiser's last save. Bumped by every PUT the resource issues, so plans that produce an Update will show this as `(known after apply)`.",
				Computed:            true,
				// Intentionally no UseStateForUnknown: every Update bumps
				// time_saved server-side, so the planned value genuinely
				// is unknown until after apply.
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name of the advertiser. Shown in the Everflow UI and used on tracking links and reports.",
				Required:            true,
			},
			"account_status": schema.StringAttribute{
				MarkdownDescription: "Account status. One of `active`, `inactive`, `suspended`. `terraform destroy` sets this to `inactive` — see the resource description for soft-delete semantics.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("active", "inactive", "suspended"),
				},
			},
			"network_employee_id": schema.Int64Attribute{
				MarkdownDescription: "ID of the network employee who owns the advertiser account.",
				Required:            true,
			},
			"default_currency_id": schema.StringAttribute{
				MarkdownDescription: "ISO 4217 currency code used as the default for this advertiser (e.g. `USD`, `EUR`).",
				Required:            true,
			},
			"reporting_timezone_id": schema.Int64Attribute{
				MarkdownDescription: "Everflow timezone ID used for the advertiser's reports (e.g. `80` = America/New_York). See the Everflow timezone reference for the full list.",
				Required:            true,
			},
			"internal_notes": schema.StringAttribute{
				MarkdownDescription: "Free-form notes visible only to network employees. A good place for Terraform-managed markers (e.g. `Managed by Terraform — do not edit in UI`).",
				Optional:            true,
			},
		},
	}
}

// Configure wires the shared *everflow.Client into the resource. Called once
// per resource instance during provider startup.
func (r *AdvertiserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create issues a POST to Everflow and writes the decoded response (plus all
// computed fields) back to Terraform state.
func (r *AdvertiserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AdvertiserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := everflow.CreateAdvertiserInput{
		Name:                plan.Name.ValueString(),
		AccountStatus:       plan.AccountStatus.ValueString(),
		NetworkEmployeeID:   plan.NetworkEmployeeID.ValueInt64(),
		DefaultCurrencyID:   plan.DefaultCurrencyID.ValueString(),
		ReportingTimezoneID: plan.ReportingTimezoneID.ValueInt64(),
		InternalNotes:       plan.InternalNotes.ValueString(),
	}

	created, err := r.client.CreateAdvertiser(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Everflow advertiser", err.Error())
		return
	}

	writeAdvertiserToModel(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the local state from Everflow. A 404 is interpreted as
// "the advertiser was deleted out-of-band"; in that case we remove the
// resource from state instead of surfacing an error, which is the
// framework-idiomatic behavior.
func (r *AdvertiserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AdvertiserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkAdvertiserID.ValueInt64()
	got, err := r.client.GetAdvertiser(ctx, id)
	if err != nil {
		if everflow.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read Everflow advertiser", err.Error())
		return
	}

	writeAdvertiserToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements the fetch-modify-put strategy: GET the raw map, overlay
// the schema-managed fields with the plan values, PUT the merged body. This
// keeps unmodeled fields (billing, contact_address, settings, users, ...)
// intact across Terraform-managed updates.
func (r *AdvertiserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AdvertiserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state AdvertiserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkAdvertiserID.ValueInt64()
	merged, err := r.fetchAndOverlay(ctx, id, plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch existing Everflow advertiser for update", err.Error())
		return
	}

	updated, err := r.client.UpdateAdvertiser(ctx, id, merged)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update Everflow advertiser", err.Error())
		return
	}

	writeAdvertiserToModel(updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete performs a soft delete: fetch the raw record, overlay
// account_status = "inactive" (and ONLY that field), PUT the merged body,
// and let the framework remove the resource from state. The record
// persists in Everflow as an inactive advertiser.
//
// Unlike Update, Delete does not reconcile other schema-managed fields
// back to their state values. If the user made out-of-band changes to,
// say, `name` before running `terraform destroy`, those edits are
// preserved — the only field the destroy PUT touches is `account_status`.
// This mirrors the intent of a destroy operation ("I'm done with this
// resource, stop touching it") rather than a full rewrite.
func (r *AdvertiserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AdvertiserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.NetworkAdvertiserID.ValueInt64()

	raw, err := r.client.GetAdvertiserRaw(ctx, id)
	if err != nil {
		if everflow.IsNotFound(err) {
			// Already gone server-side — nothing to deactivate.
			return
		}
		resp.Diagnostics.AddError("Failed to fetch Everflow advertiser for soft-delete", err.Error())
		return
	}
	raw["account_status"] = "inactive"

	if _, err := r.client.UpdateAdvertiser(ctx, id, raw); err != nil {
		resp.Diagnostics.AddError("Failed to soft-delete Everflow advertiser", err.Error())
		return
	}
}

// ImportState lets `terraform import` accept a numeric network_advertiser_id
// on the command line. The framework's default passthrough would write a
// string into the ID field; we parse it to int64 first so the value lands
// in the correct typed attribute.
func (r *AdvertiserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("network_advertiser_id must be a base-10 integer; got %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_advertiser_id"), id)...)
}

// fetchAndOverlay is the shared plumbing for Update and Delete. It GETs the
// raw advertiser from Everflow and overlays the schema-managed fields from
// plan onto the raw map. Fields outside the typed schema are preserved
// untouched so the resulting map can be PUT back without clobbering unmodeled
// server state.
func (r *AdvertiserResource) fetchAndOverlay(ctx context.Context, id int64, plan AdvertiserResourceModel) (map[string]any, error) {
	raw, err := r.client.GetAdvertiserRaw(ctx, id)
	if err != nil {
		return nil, err
	}

	raw["name"] = plan.Name.ValueString()
	raw["account_status"] = plan.AccountStatus.ValueString()
	raw["network_employee_id"] = plan.NetworkEmployeeID.ValueInt64()
	raw["default_currency_id"] = plan.DefaultCurrencyID.ValueString()
	raw["reporting_timezone_id"] = plan.ReportingTimezoneID.ValueInt64()

	// Optional field: write an explicit empty string when the plan is
	// null. Everflow's PUT is full replacement, so *omitting* the key
	// from the body would leave a stale value behind on the server; we
	// need to send "" to actually clear it. Read-side normalization
	// (writeAdvertiserToModel) maps "" back to null so the null round-
	// trip is drift-free.
	raw["internal_notes"] = plan.InternalNotes.ValueString()

	return raw, nil
}

// writeAdvertiserToModel copies server values into a Terraform model in
// place. It's used by Create, Read, and Update — any computed field handled
// by the framework's UseStateForUnknown plan modifier needs to be populated
// here or the framework will panic with "unknown after apply".
func writeAdvertiserToModel(src everflow.Advertiser, dst *AdvertiserResourceModel) {
	dst.NetworkAdvertiserID = types.Int64Value(src.NetworkAdvertiserID)
	dst.NetworkID = types.Int64Value(src.NetworkID)
	dst.TimeCreated = types.Int64Value(src.TimeCreated)
	dst.TimeSaved = types.Int64Value(src.TimeSaved)
	dst.Name = types.StringValue(src.Name)
	dst.AccountStatus = types.StringValue(src.AccountStatus)
	dst.NetworkEmployeeID = types.Int64Value(src.NetworkEmployeeID)
	dst.DefaultCurrencyID = types.StringValue(src.DefaultCurrencyID)
	dst.ReportingTimezoneID = types.Int64Value(src.ReportingTimezoneID)
	if src.InternalNotes == "" {
		dst.InternalNotes = types.StringNull()
	} else {
		dst.InternalNotes = types.StringValue(src.InternalNotes)
	}
}
