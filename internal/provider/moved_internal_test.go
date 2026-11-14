package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// providerResource is the provider's resource of a type, as Terraform gets
// it: wrappers and all.
func providerResource(t *testing.T, typeName string) (resource.Resource, resource.SchemaResponse) {
	t.Helper()
	ctx := context.Background()
	for _, factory := range (&AuthwiseProvider{}).Resources(ctx) {
		r := factory()
		md := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, md)
		if md.TypeName == typeName {
			s := resource.SchemaResponse{}
			r.Schema(ctx, resource.SchemaRequest{}, &s)
			require.False(t, s.Diagnostics.HasError(), "%v", s.Diagnostics)
			return r, s
		}
	}
	t.Fatalf("no resource %s", typeName)
	return nil, resource.SchemaResponse{}
}

// move runs a resource's movers over a former state, as `moved` does.
func move(t *testing.T, typeName, sourceType, sourceAddress, raw string) resource.MoveStateResponse {
	t.Helper()
	ctx := context.Background()
	r, s := providerResource(t, typeName)
	m, ok := r.(resource.ResourceWithMoveState)
	require.True(t, ok, "%s takes no moved state", typeName)
	resp := resource.MoveStateResponse{TargetState: tfsdk.State{
		Schema: s.Schema,
		Raw:    tftypes.NewValue(s.Schema.Type().TerraformType(ctx), nil),
	}}
	for _, mover := range m.MoveState(ctx) {
		mover.StateMover(ctx, resource.MoveStateRequest{
			SourceTypeName:        sourceType,
			SourceProviderAddress: sourceAddress,
			SourceRawState:        &tfprotov6.RawState{JSON: []byte(raw)},
		}, &resp)
	}
	return resp
}

func stringAt(t *testing.T, state tfsdk.State, attr string) string {
	t.Helper()
	var v types.String
	require.False(t, state.GetAttribute(context.Background(), path.Root(attr), &v).HasError(), attr)
	return v.ValueString()
}

const registryAddress = "registry.terraform.io/authwisecom/authwise"

// TestMoved_Client: authwise_client's state, as v0.34 wrote it, becomes
// authwise_application's: its id and variant under their new names, its
// name at kit's new collection, the rest as it was.
func TestMoved_Client(t *testing.T) {
	resp := move(t, applicationTypeName, "authwise_client", registryAddress, `{
		"name": "tenants/t-01/issuers/i-01/clients/c-01",
		"client_id": "c-01",
		"tenant_id": "t-01",
		"issuer_id": "i-01",
		"display_name": "Console",
		"appearance_profile_id": "ap-01",
		"grant_types": ["authorization_code"]
	}`)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.Equal(t, "tenants/t-01/issuers/i-01/applications/c-01", stringAt(t, resp.TargetState, "name"))
	assert.Equal(t, "c-01", stringAt(t, resp.TargetState, "application_id"))
	assert.Equal(t, "ap-01", stringAt(t, resp.TargetState, "theme_variant_id"))
	assert.Equal(t, "Console", stringAt(t, resp.TargetState, "display_name"))
	assert.Equal(t, "i-01", stringAt(t, resp.TargetState, "issuer_id"))
}

// TestMoved_ClientSecret: the secret moves with its value, which kit never
// returns again.
func TestMoved_ClientSecret(t *testing.T) {
	resp := move(t, applicationSecretTypeName, "authwise_client_secret", registryAddress, `{
		"name": "tenants/t-01/issuers/i-01/clients/c-01/client-secrets/cs-01",
		"client_id": "c-01",
		"tenant_id": "t-01",
		"issuer_id": "i-01",
		"secret": "s3cret",
		"expires_at": "2027-01-01T00:00:00Z"
	}`)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.Equal(t, "tenants/t-01/issuers/i-01/applications/c-01/application-secrets/cs-01", stringAt(t, resp.TargetState, "name"))
	assert.Equal(t, "c-01", stringAt(t, resp.TargetState, "application_id"))
	assert.Equal(t, "s3cret", stringAt(t, resp.TargetState, "secret"))
}

// TestMoved_AppearanceProfile: the profile becomes a theme variant, its
// default flag kept.
func TestMoved_AppearanceProfile(t *testing.T) {
	resp := move(t, themeVariantTypeName, "authwise_appearance_profile", registryAddress, `{
		"name": "tenants/t-01/issuers/i-01/appearance-profiles/ap-01",
		"appearance_profile_id": "ap-01",
		"theme_id": "th-01",
		"is_default": true
	}`)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.Equal(t, "tenants/t-01/issuers/i-01/theme-variants/ap-01", stringAt(t, resp.TargetState, "name"))
	assert.Equal(t, "ap-01", stringAt(t, resp.TargetState, "theme_variant_id"))
	var isDefault types.Bool
	require.False(t, resp.TargetState.GetAttribute(context.Background(), path.Root("is_default"), &isDefault).HasError())
	assert.True(t, isDefault.ValueBool())
}

// TestMoved_NotOurs: another type or another provider's resource is not
// moved, so Terraform reports that no move was found.
func TestMoved_NotOurs(t *testing.T) {
	for _, c := range []struct{ source, address string }{
		{"authwise_audience", registryAddress},
		{"authwise_client", "registry.terraform.io/hashicorp/random"},
	} {
		resp := move(t, applicationTypeName, c.source, c.address, `{"name": "x"}`)
		assert.False(t, resp.Diagnostics.HasError())
		assert.True(t, resp.TargetState.Raw.IsNull(), "%s from %s moved", c.source, c.address)
	}
}

// TestMoved_SegmentsOnlyAtCollections: an id that happens to spell a
// collection is left alone.
func TestMoved_SegmentsOnlyAtCollections(t *testing.T) {
	b, err := applicationRenamedFrom.renameState([]byte(`{"name": "tenants/clients/issuers/i-01/clients/c-01"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"name": "tenants/clients/issuers/i-01/applications/c-01"}`, string(b))
}
