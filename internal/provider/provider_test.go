package provider_test

import (
	"context"
	"reflect"
	"testing"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
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
	// 20 entity resources + 5 association resources (user_roles,
	// client_roles, role_permissions, scope_permissions,
	// access_role_access_permissions).
	assert.Len(t, resourceTypes, 25)
	for _, want := range []string{
		"authwise_access_permission",
		"authwise_access_role",
		"authwise_access_role_access_permissions",
		"authwise_access_condition",
		"authwise_access_binding",
		"authwise_certificate",
	} {
		assert.True(t, resourceTypes[want], "missing resource %s", want)
	}

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
	// 20 singular entity data sources + 12 config builder data sources.
	assert.Len(t, dataSourceTypes, 32)
	for _, want := range []string{
		"authwise_certificate",
		"authwise_provider_saml",
		"authwise_saml_relying_party_config",
	} {
		assert.True(t, dataSourceTypes[want], "missing data source %s", want)
	}
}

// TestAccessBindingIsWrapped asserts the substitution actually happened:
// authwise_access_binding must be the wrapper carrying the role-reference
// check, not the bare generated resource, and it must still present the
// generated schema.
func TestAccessBindingIsWrapped(t *testing.T) {

	ctx := context.Background()
	p := provider.New("test")()

	var found resource.Resource
	for _, newResource := range p.Resources(ctx) {
		r := newResource()
		m := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "authwise"}, m)
		if m.TypeName == "authwise_access_binding" {
			found = r
		}
	}

	require.NotNil(t, found)
	assert.NotEqual(t, reflect.TypeOf(generated.NewAccessBindingResource()), reflect.TypeOf(found),
		"authwise_access_binding must be the validating wrapper")

	s := &resource.SchemaResponse{}
	found.Schema(ctx, resource.SchemaRequest{}, s)
	require.False(t, s.Diagnostics.HasError(), s.Diagnostics)
	assert.Contains(t, s.Schema.Attributes, "role_name")
}
