package provider

import (
	"context"
	"testing"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// networkStateWithTenant is an authwise_guard_network state holding
// tenantID, as a state written before the tenant collapse does.
func networkStateWithTenant(ctx context.Context, t *testing.T, r resource.Resource, tenantID string) tfsdk.State {
	t.Helper()
	sr := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, sr)
	require.False(t, sr.Diagnostics.HasError(), sr.Diagnostics)
	state := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
	require.False(t, state.SetAttribute(ctx, path.Root(tenantIDAttribute), tenantID).HasError())
	require.False(t, state.SetAttribute(ctx, path.Root("name"), "tenants/"+tenantID+"/networks/n-1").HasError())
	return state
}

// A state still holding a Guard tenant's id fails the refresh with the
// upgrade note, before guard-control is asked: its NotFound would drop the
// resource and the plan would recreate it (tf#65).
func TestGuardStateWithGuardTenantIsRefused(t *testing.T) {

	ctx := context.Background()
	r := &guardResource{inner: generated.NewNetworkResource(), configured: true}
	state := networkStateWithTenant(ctx, t, r, "gt-acme")

	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	require.True(t, resp.Diagnostics.HasError())
	detail := resp.Diagnostics.Errors()[0].Detail()
	assert.Contains(t, detail, "gt-acme")
	assert.Contains(t, detail, "terraform state rm")
	assert.False(t, resp.State.Raw.IsNull(), "the resource must stay in state")

	plan := &resource.ModifyPlanResponse{Plan: tfsdk.Plan(state)}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{State: state, Plan: plan.Plan}, plan)
	assert.True(t, plan.Diagnostics.HasError(), "a plan with -refresh=false is refused too")
}

func TestGuardTenantUpgradeHintOnlyForGuardTenants(t *testing.T) {
	assert.Empty(t, guardTenantUpgradeHint("t-1"))
	assert.Contains(t, guardTenantUpgradeHint("gt-1"), "authwise_guard_tenant_settings")
}
