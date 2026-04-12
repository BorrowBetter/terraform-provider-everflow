// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BorrowBetter/terraform-provider-everflow/internal/everflow"
)

// Ensure EverflowProvider satisfies the provider.Provider interface.
var _ provider.Provider = &EverflowProvider{}

// EverflowProvider is the Terraform provider implementation for Everflow.
type EverflowProvider struct {
	// version is set during build time and exposed to resources for diagnostics.
	version string
}

// EverflowProviderModel maps provider configuration schema to a Go type.
type EverflowProviderModel struct {
	APIKey  types.String `tfsdk:"api_key"`
	BaseURL types.String `tfsdk:"base_url"`
}

// New returns a factory that constructs a fresh EverflowProvider on each call.
// The Plugin Framework calls this factory during planning and applying.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &EverflowProvider{version: version}
	}
}

// Metadata sets the provider type name (prefix for all resources) and version.
func (p *EverflowProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "everflow"
	resp.Version = p.version
}

// Schema declares the provider-level configuration block.
func (p *EverflowProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Everflow provider manages affiliate marketing resources (advertisers, offers, affiliates) in an Everflow network account via the Everflow Network API.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Everflow Network API key sent as the `X-Eflow-Api-Key` header on every request. May also be set via the `EVERFLOW_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Base URL for the Everflow Network API. Defaults to `https://api.eflow.team`. Primarily useful for test overrides.",
				Optional:            true,
			},
		},
	}
}

// Configure reads provider configuration, instantiates the Everflow API client,
// and publishes it to resources and data sources via ResourceData / DataSourceData.
func (p *EverflowProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data EverflowProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve the API key: config > environment variable.
	apiKey := data.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("EVERFLOW_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			schemaPath("api_key"),
			"Missing Everflow API key",
			"Set the `api_key` provider attribute or the `EVERFLOW_API_KEY` environment variable.",
		)
		return
	}

	baseURL := data.BaseURL.ValueString()

	client := everflow.New(apiKey, baseURL, p.version)
	resp.ResourceData = client
	resp.DataSourceData = client
}

// Resources returns the list of resource type constructors this provider
// exposes. New resources are added here.
func (p *EverflowProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAdvertiserResource,
		NewAffiliateResource,
		NewOfferResource,
	}
}

// DataSources returns the list of data source type constructors this provider
// exposes. v0.1.0 ships no data sources.
func (p *EverflowProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
