// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"gitlab.authwise.io/authwise/api-client-go/authwise/management"
)

// Ensure AuthwiseProvider satisfies various provider interfaces.
var _ provider.Provider = &AuthwiseProvider{}

// AuthwiseProvider defines the provider implementation.
type AuthwiseProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// AuthwiseProviderModel describes the provider data model.
type AuthwiseProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
}

func (p *AuthwiseProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "authwise"
	resp.Version = p.version
}

func (p *AuthwiseProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Realm provider attribute",
				Optional:            true,
			},
		},
	}
}

func (p *AuthwiseProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data AuthwiseProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values are now available.
	// if data.Endpoint.IsNull() { /* ... */ }

	// Realm client configuration for data sources and resources

	client, err := management.NewDefaultClient("localhost:8899", true)

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create client",
			err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *AuthwiseProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRealmResource,
	}
}

func (p *AuthwiseProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewRealmDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AuthwiseProvider{
			version: version,
		}
	}
}
