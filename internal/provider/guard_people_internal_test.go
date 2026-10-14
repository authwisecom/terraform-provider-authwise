package provider

import (
	"context"
	"testing"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubTenantAdmin records what reached guard-control's tenant admin service.
type stubTenantAdmin struct {
	guardpb.GuardTenantAdminServiceClient

	grants   []*guardpb.EnrolmentGrant
	granted  []*guardpb.GrantEnrolmentRequest
	revoked  []*guardpb.RevokeEnrolmentRequest
	grantErr error

	deprovisioning *guardpb.Deprovisioning
	set            []*guardpb.SetDeprovisioningAccountRequest
	cleared        int
}

func (s *stubTenantAdmin) GrantEnrolment(_ context.Context, in *guardpb.GrantEnrolmentRequest, _ ...grpc.CallOption) (*guardpb.EnrolmentGrant, error) {
	if s.grantErr != nil {
		return nil, s.grantErr
	}
	s.granted = append(s.granted, in)
	return &guardpb.EnrolmentGrant{Binding: grantBinding, Subject: in.GetSubject(), ResourceType: enrolmentAnchorTenant, ResourceId: "gt-1", DisplayName: "Ada"}, nil
}

func (s *stubTenantAdmin) ListEnrolmentGrants(_ context.Context, _ *guardpb.ListEnrolmentGrantsRequest, _ ...grpc.CallOption) (*guardpb.ListEnrolmentGrantsResponse, error) {
	return &guardpb.ListEnrolmentGrantsResponse{Grants: s.grants}, nil
}

func (s *stubTenantAdmin) RevokeEnrolment(_ context.Context, in *guardpb.RevokeEnrolmentRequest, _ ...grpc.CallOption) (*guardpb.RevokeEnrolmentResponse, error) {
	s.revoked = append(s.revoked, in)
	return &guardpb.RevokeEnrolmentResponse{}, nil
}

func (s *stubTenantAdmin) GetDeprovisioning(_ context.Context, _ *guardpb.GetDeprovisioningRequest, _ ...grpc.CallOption) (*guardpb.Deprovisioning, error) {
	return s.deprovisioning, nil
}

func (s *stubTenantAdmin) SetDeprovisioningAccount(_ context.Context, in *guardpb.SetDeprovisioningAccountRequest, _ ...grpc.CallOption) (*guardpb.Deprovisioning, error) {
	s.set = append(s.set, in)
	return &guardpb.Deprovisioning{Available: true, Configured: true}, nil
}

func (s *stubTenantAdmin) ClearDeprovisioningAccount(_ context.Context, _ *guardpb.ClearDeprovisioningAccountRequest, _ ...grpc.CallOption) (*guardpb.Deprovisioning, error) {
	s.cleared++
	return &guardpb.Deprovisioning{Available: true}, nil
}

const grantBinding = "audiences/a-1/access-bindings/ab-1"

// stateOf is a state, plan or config value of r's schema holding m.
func stateOf(ctx context.Context, t *testing.T, r resource.Resource, m any) tfsdk.State {
	t.Helper()
	sr := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, sr)
	st := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
	require.False(t, st.Set(ctx, m).HasError())
	return st
}

func emptyState(ctx context.Context, t *testing.T, r resource.Resource) tfsdk.State {
	t.Helper()
	sr := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, sr)
	return tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
}

func grantModel(binding string) *guardEnrolmentGrantModel {
	m := &guardEnrolmentGrantModel{}
	m.TenantID = types.StringValue("gt-1")
	m.SubjectType = types.StringValue(enrolmentSubjectUser)
	m.SubjectID = types.StringValue("u-1")
	m.Binding = types.StringValue(binding)
	m.DisplayName = types.StringValue("")
	m.Email = types.StringValue("")
	return m
}

func TestEnrolmentGrantCreateSendsSubject(t *testing.T) {

	ctx := context.Background()
	stub := &stubTenantAdmin{}
	r := &guardEnrolmentGrantResource{client: stub}

	plan := stateOf(context.Background(), t, r, grantModel(""))
	resp := &resource.CreateResponse{State: emptyState(context.Background(), t, r)}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	require.Len(t, stub.granted, 1)
	assert.Equal(t, "tenants/gt-1", stub.granted[0].GetName())
	assert.Equal(t, "user", stub.granted[0].GetSubject().GetType())
	assert.Equal(t, "u-1", stub.granted[0].GetSubject().GetId())

	var got guardEnrolmentGrantModel
	require.False(t, resp.State.Get(ctx, &got).HasError())
	assert.Equal(t, grantBinding, got.Binding.ValueString())
	assert.Equal(t, "Ada", got.DisplayName.ValueString())
}

func TestEnrolmentGrantPermissionDeniedSaysWhoMay(t *testing.T) {

	stub := &stubTenantAdmin{grantErr: status.Error(codes.PermissionDenied, "the users-link home refused")}
	r := &guardEnrolmentGrantResource{client: stub}

	resp := &resource.CreateResponse{State: emptyState(context.Background(), t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(stateOf(context.Background(), t, r, grantModel("")))}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "identity server 1.46.0")
}

func TestEnrolmentGrantRead(t *testing.T) {

	cases := map[string]struct {
		grants  []*guardpb.EnrolmentGrant
		removed bool
		fails   bool
	}{
		"found": {grants: []*guardpb.EnrolmentGrant{
			{Binding: "other"},
			{Binding: grantBinding, Subject: &guardpb.EnrolmentSubject{Type: "user", Id: "u-1"}, ResourceType: enrolmentAnchorTenant, Email: "ada@example.com"},
		}},
		"gone": {grants: []*guardpb.EnrolmentGrant{{Binding: "other"}}, removed: true},
		"at a network": {grants: []*guardpb.EnrolmentGrant{
			{Binding: grantBinding, Subject: &guardpb.EnrolmentSubject{Type: "user", Id: "u-1"}, ResourceType: "guardcontrol.network", ResourceId: "n-1"},
		}, fails: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {

			ctx := context.Background()
			r := &guardEnrolmentGrantResource{client: &stubTenantAdmin{grants: c.grants}}

			state := stateOf(context.Background(), t, r, grantModel(grantBinding))
			resp := &resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, resp)

			require.Equal(t, c.fails, resp.Diagnostics.HasError(), resp.Diagnostics)
			if c.fails {
				return
			}
			assert.Equal(t, c.removed, resp.State.Raw.IsNull())
			if !c.removed {
				var got guardEnrolmentGrantModel
				require.False(t, resp.State.Get(ctx, &got).HasError())
				assert.Equal(t, "ada@example.com", got.Email.ValueString())
			}
		})
	}
}

func TestEnrolmentGrantDeleteRevokesBinding(t *testing.T) {

	stub := &stubTenantAdmin{}
	r := &guardEnrolmentGrantResource{client: stub}

	state := stateOf(context.Background(), t, r, grantModel(grantBinding))
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	require.Len(t, stub.revoked, 1)
	assert.Equal(t, "tenants/gt-1", stub.revoked[0].GetName())
	assert.Equal(t, grantBinding, stub.revoked[0].GetBinding())
}

func TestEnrolmentGrantImportID(t *testing.T) {

	ctx := context.Background()
	r := &guardEnrolmentGrantResource{}

	resp := &resource.ImportStateResponse{State: emptyState(context.Background(), t, r)}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "gt-1/" + grantBinding}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	var got guardEnrolmentGrantModel
	require.False(t, resp.State.Get(ctx, &got).HasError())
	assert.Equal(t, "gt-1", got.TenantID.ValueString())
	assert.Equal(t, grantBinding, got.Binding.ValueString())

	for _, bad := range []string{grantBinding, "gt-1", "gt-1/", "t-1/" + grantBinding} {
		resp := &resource.ImportStateResponse{State: emptyState(context.Background(), t, r)}
		r.ImportState(ctx, resource.ImportStateRequest{ID: bad}, resp)
		assert.True(t, resp.Diagnostics.HasError(), bad)
	}
}

func TestEnrolmentGrantSubjectIDMatchesType(t *testing.T) {

	r := &guardEnrolmentGrantResource{}

	for id, fails := range map[string]bool{"u-1": false, "g-1": true} {
		resp := &resource.ValidateConfigResponse{}
		m := grantModel("")
		m.SubjectID = types.StringValue(id)
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(stateOf(context.Background(), t, r, m))}, resp)
		assert.Equal(t, fails, resp.Diagnostics.HasError(), id)
	}
}

func deprovisioningModel() *guardDeprovisioningModel {
	return &guardDeprovisioningModel{
		TenantID:              types.StringValue("gt-1"),
		ClientID:              types.StringValue("sa-1"),
		ClientSecretWO:        types.StringValue("s3cret"),
		ClientSecretWOVersion: types.Int64Null(),
		TokenURL:              types.StringValue("https://id.example.com/oauth2/token"),
		Audience:              types.StringValue("https://admin.example.com"),
	}
}

func TestDeprovisioningCreateSendsSecretAndKeepsItOutOfState(t *testing.T) {

	ctx := context.Background()
	stub := &stubTenantAdmin{deprovisioning: &guardpb.Deprovisioning{Available: true}}
	r := &guardDeprovisioningResource{client: stub}

	cfg := stateOf(context.Background(), t, r, deprovisioningModel())
	resp := &resource.CreateResponse{State: emptyState(context.Background(), t, r)}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(cfg), Config: tfsdk.Config(cfg)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	require.Len(t, stub.set, 1)
	assert.Equal(t, "tenants/gt-1", stub.set[0].GetName())
	assert.Equal(t, "s3cret", stub.set[0].GetClientSecret())

	var got guardDeprovisioningModel
	require.False(t, resp.State.Get(ctx, &got).HasError())
	assert.True(t, got.ClientSecretWO.IsNull())
}

func TestDeprovisioningCreateRefusedWhenUnavailable(t *testing.T) {

	stub := &stubTenantAdmin{deprovisioning: &guardpb.Deprovisioning{}}
	r := &guardDeprovisioningResource{client: stub}

	cfg := stateOf(context.Background(), t, r, deprovisioningModel())
	resp := &resource.CreateResponse{State: emptyState(context.Background(), t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(cfg), Config: tfsdk.Config(cfg)}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "sweep.sealKeyFile")
	assert.Empty(t, stub.set)
}

func TestDeprovisioningRead(t *testing.T) {

	ctx := context.Background()

	m := deprovisioningModel()
	m.ClientSecretWO = types.StringNull()

	t.Run("cleared elsewhere", func(t *testing.T) {
		r := &guardDeprovisioningResource{client: &stubTenantAdmin{deprovisioning: &guardpb.Deprovisioning{Available: true}}}
		state := stateOf(ctx, t, r, m)
		resp := &resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, resp)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, resp.State.Raw.IsNull())
	})

	t.Run("changed elsewhere", func(t *testing.T) {
		r := &guardDeprovisioningResource{client: &stubTenantAdmin{deprovisioning: &guardpb.Deprovisioning{
			Available: true, Configured: true, ClientId: "sa-2", TokenUrl: m.TokenURL.ValueString(), Audience: m.Audience.ValueString(),
		}}}
		state := stateOf(ctx, t, r, m)
		resp := &resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, resp)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		var got guardDeprovisioningModel
		require.False(t, resp.State.Get(ctx, &got).HasError())
		assert.Equal(t, "sa-2", got.ClientID.ValueString())
	})
}

func TestDeprovisioningDeleteClears(t *testing.T) {

	stub := &stubTenantAdmin{}
	r := &guardDeprovisioningResource{client: stub}

	m := deprovisioningModel()
	m.ClientSecretWO = types.StringNull()
	state := stateOf(context.Background(), t, r, m)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	assert.Equal(t, 1, stub.cleared)
}

// guardWrap must not hide the grant's subject check behind its wrapper.
func TestGuardWrapKeepsValidateConfig(t *testing.T) {

	wrapped := guardWrap(context.Background(), []func() resource.Resource{newGuardEnrolmentGrantResource})[0]()
	v, ok := wrapped.(resource.ResourceWithValidateConfig)
	require.True(t, ok)

	r := &guardEnrolmentGrantResource{}
	m := grantModel("")
	m.SubjectID = types.StringValue("g-1")
	resp := &resource.ValidateConfigResponse{}
	v.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(stateOf(context.Background(), t, r, m))}, resp)
	assert.True(t, resp.Diagnostics.HasError())
}
