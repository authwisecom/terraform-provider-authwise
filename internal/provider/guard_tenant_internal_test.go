package provider

import (
	"context"
	"testing"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubDirectory answers ResolveTenant for one slug.
type stubDirectory struct {
	guardpb.GuardDirectoryServiceClient

	slug, id string
	asked    []*guardpb.ResolveTenantRequest
}

func (s *stubDirectory) ResolveTenant(_ context.Context, in *guardpb.ResolveTenantRequest, _ ...grpc.CallOption) (*guardpb.ResolvedTenant, error) {
	s.asked = append(s.asked, in)
	if in.GetSlug() != s.slug {
		return nil, status.Error(codes.NotFound, "no such tenant")
	}
	return &guardpb.ResolvedTenant{Id: s.id, Slug: s.slug}, nil
}

// recordingTenantRead stands in for the generated read by name: it records
// the name it was asked for.
type recordingTenantRead struct {
	names []string
}

func (r *recordingTenantRead) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guard_tenant"
}

func (r *recordingTenantRead) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = generated.TenantDataSourceSchema()
}

func (r *recordingTenantRead) Read(ctx context.Context, req datasource.ReadRequest, _ *datasource.ReadResponse) {
	var name types.String
	req.Config.GetAttribute(ctx, path.Root("name"), &name)
	r.names = append(r.names, name.ValueString())
}

// tenantConfig is a config of the wrapped schema setting attr to v.
func tenantConfig(ctx context.Context, t *testing.T, d datasource.DataSource, attr, v string) tfsdk.Config {
	t.Helper()
	sr := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, sr)
	st := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
	require.False(t, st.SetAttribute(ctx, path.Root(attr), v).HasError())
	return tfsdk.Config{Schema: sr.Schema, Raw: st.Raw}
}

func TestGuardTenantBySlugReadsByResolvedName(t *testing.T) {

	ctx := context.Background()
	inner := &recordingTenantRead{}
	dir := &stubDirectory{slug: "millcreek", id: "gt-7"}
	d := &guardTenantDataSource{inner: inner, directory: dir}

	resp := &datasource.ReadResponse{}
	d.Read(ctx, datasource.ReadRequest{Config: tenantConfig(ctx, t, d, "slug", "millcreek")}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	require.Len(t, dir.asked, 1)
	assert.Equal(t, "millcreek", dir.asked[0].GetSlug())
	assert.Equal(t, []string{"tenants/gt-7"}, inner.names)
}

func TestGuardTenantUnknownSlugFails(t *testing.T) {

	ctx := context.Background()
	inner := &recordingTenantRead{}
	d := &guardTenantDataSource{inner: inner, directory: &stubDirectory{slug: "millcreek", id: "gt-7"}}

	resp := &datasource.ReadResponse{}
	d.Read(ctx, datasource.ReadRequest{Config: tenantConfig(ctx, t, d, "slug", "acme")}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "no Guard tenant has the slug acme")
	assert.Empty(t, inner.names)
}

func TestGuardTenantByNameDoesNotResolve(t *testing.T) {

	ctx := context.Background()
	inner := &recordingTenantRead{}
	dir := &stubDirectory{}
	d := &guardTenantDataSource{inner: inner, directory: dir}

	resp := &datasource.ReadResponse{}
	d.Read(ctx, datasource.ReadRequest{Config: tenantConfig(ctx, t, d, "name", "tenants/gt-1")}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	assert.Empty(t, dir.asked)
	assert.Equal(t, []string{"tenants/gt-1"}, inner.names)
}

// The provider serves authwise_guard_tenant through the wrapper.
func TestGuardTenantDataSourceIsWrapped(t *testing.T) {

	ctx := context.Background()
	for _, f := range (&AuthwiseProvider{}).DataSources(ctx) {
		d := f()
		resp := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, resp)
		if resp.TypeName != guardTenantTypeName {
			continue
		}
		sr := &datasource.SchemaResponse{}
		d.Schema(ctx, datasource.SchemaRequest{}, sr)
		assert.True(t, sr.Schema.Attributes["slug"].IsOptional())
		assert.True(t, sr.Schema.Attributes["name"].IsOptional())
		return
	}
	t.Fatal("authwise_guard_tenant is not registered")
}
