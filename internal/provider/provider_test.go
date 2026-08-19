package provider_test

import (
	"context"
	"testing"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProviderSurface validates the provider schema plus every generated
// resource and data source schema: metadata resolves under the provider
// type name and each schema passes the framework's implementation
// validation. Full lifecycle coverage runs against a real kit server in the
// acceptance suite.
func TestProviderSurface(t *testing.T) {

	ctx := context.Background()
	p := provider.New("test")()

	schemaResp := &fwprovider.SchemaResponse{}
	p.Schema(ctx, fwprovider.SchemaRequest{}, schemaResp)
	require.False(t, schemaResp.Diagnostics.HasError(), schemaResp.Diagnostics)
	diags := schemaResp.Schema.ValidateImplementation(ctx)
	require.False(t, diags.HasError(), diags)

	metaResp := &fwprovider.MetadataResponse{}
	p.Metadata(ctx, fwprovider.MetadataRequest{}, metaResp)
	require.Equal(t, "authwise", metaResp.TypeName)

	resourceTypes := map[string]bool{}
	for _, newResource := range p.Resources(ctx) {

		r := newResource()

		m := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "authwise"}, m)
		assert.NotEmpty(t, m.TypeName)
		assert.False(t, resourceTypes[m.TypeName], "duplicate resource type %s", m.TypeName)
		resourceTypes[m.TypeName] = true

		s := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, s)
		require.False(t, s.Diagnostics.HasError(), s.Diagnostics)
		diags := s.Schema.ValidateImplementation(ctx)
		require.False(t, diags.HasError(), "%s: %v", m.TypeName, diags)
	}
	// 15 entity resources + 4 association resources (user_roles,
	// client_roles, role_permissions, scope_permissions).
	assert.Len(t, resourceTypes, 19)

	dataSourceTypes := map[string]bool{}
	for _, newDataSource := range p.DataSources(ctx) {

		d := newDataSource()

		m := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "authwise"}, m)
		assert.NotEmpty(t, m.TypeName)
		assert.False(t, dataSourceTypes[m.TypeName], "duplicate data source type %s", m.TypeName)
		dataSourceTypes[m.TypeName] = true

		s := &datasource.SchemaResponse{}
		d.Schema(ctx, datasource.SchemaRequest{}, s)
		require.False(t, s.Diagnostics.HasError(), s.Diagnostics)
		diags := s.Schema.ValidateImplementation(ctx)
		require.False(t, diags.HasError(), "%s: %v", m.TypeName, diags)
	}
	// 15 singular entity data sources + 10 config builder data sources.
	assert.Len(t, dataSourceTypes, 25)

}
