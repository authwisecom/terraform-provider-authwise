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
	// 19 generated entity resources (client_secret, minted, came with
	// kit#617), 2 association resources (scope_access_permissions,
	// access_role_access_permissions), and the hand-written secret,
	// realm_authentication_policy and asset_content. Guard (#32) adds five
	// entity resources and the guard_resource_nodes association; stage 2
	// the node and the hand-written guard_node_grants. access_resource_type
	// came with the guard-control v0.12.0 catalog.
	assert.Len(t, resourceTypes, 33)
	for _, want := range []string{
		"authwise_guard_tenant",
		"authwise_guard_network",
		"authwise_guard_relay",
		"authwise_guard_resource",
		"authwise_guard_resource_nodes",
		"authwise_guard_invite",
		"authwise_guard_node",
		"authwise_guard_node_grants",
		"authwise_client_secret",
		"authwise_access_permission",
		"authwise_access_role",
		"authwise_access_role_access_permissions",
		"authwise_access_condition",
		"authwise_access_resource_type",
		"authwise_access_binding",
		"authwise_certificate",
		"authwise_factor",
		"authwise_secret",
		"authwise_realm_authentication_policy",
		"authwise_asset_content",
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
	// 18 generated singular data sources, 19 plural ones (tfinfra
	// v0.0.15's DataSourceList; client_secret has only the plural, since
	// a minted resource takes no singular one), 20 config builder data sources
	// (provider_dropbox went with apis v0.8.0; provider_magic_link and
	// provider_passkey came with #21; provider_apple, provider_oidc and
	// provider_oauth with #20), and the hand-written secret and
	// realm_authentication_context_schema. Guard (#32) adds a singular and
	// a plural data source for each of its six entities, and
	// access_resource_type a pair of its own.
	assert.Len(t, dataSourceTypes, 73)
	for _, want := range []string{"authwise_guard_tenant", "authwise_guard_tenants", "authwise_guard_invites"} {
		assert.True(t, dataSourceTypes[want], "missing data source %s", want)
	}
	assert.False(t, dataSourceTypes["authwise_client_secret"], "a minted resource has no singular data source")
	for _, want := range []string{
		"authwise_client_secrets",
		"authwise_certificate",
		"authwise_secret",
		"authwise_realm_authentication_context_schema",
		"authwise_factor_totp",
		"authwise_factor_webauthn",
		"authwise_factor_duo",
		"authwise_factor_external",
		"authwise_provider_saml",
		"authwise_saml_relying_party_config",
	} {
		assert.True(t, dataSourceTypes[want], "missing data source %s", want)
	}
}

// TestWrappedResources asserts the substitutions actually happened: each
// type must be its wrapper, not the bare generated resource, and must still
// present the generated schema.
func TestWrappedResources(t *testing.T) {

	type s struct {
		generated resource.Resource
		attribute string
	}

	cases := map[string]s{
		// Carries the role-reference check.
		"authwise_access_binding": {generated: generated.NewAccessBindingResource(), attribute: "role_name"},
		// Leaves config.authentication to the policy resource.
		"authwise_realm": {generated: generated.NewRealmResource(), attribute: "config"},
		// Reports kit's policy warnings on factor writes.
		"authwise_factor": {generated: generated.NewFactorResource(), attribute: "factor_type"},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			ctx := context.Background()
			p := provider.New("test")()

			var found resource.Resource
			for _, newResource := range p.Resources(ctx) {
				r := newResource()
				m := &resource.MetadataResponse{}
				r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "authwise"}, m)
				if m.TypeName == k {
					found = r
				}
			}

			require.NotNil(t, found)
			assert.NotEqual(t, reflect.TypeOf(v.generated), reflect.TypeOf(found), "%s must be the wrapper", k)

			resp := &resource.SchemaResponse{}
			found.Schema(ctx, resource.SchemaRequest{}, resp)
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
			assert.Contains(t, resp.Schema.Attributes, v.attribute)
		})
	}
}
