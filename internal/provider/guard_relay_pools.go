package provider

import (
	"context"
	"strings"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	guardRelayPoolAccessTypeName  = "guard_relay_pool_access"
	guardAllowedRelayPoolsTypName = "guard_allowed_relay_pools"
	guardNetworkTypeName          = "authwise_guard_network"

	// relayPoolsCollection is an estate pool's collection, relay-pools/rp-…
	// (kit's kebab naming rule).
	relayPoolsCollection = "relay-pools"
	relayPoolAccessID    = "relay-pool-access"

	relayPoolAttribute = "relay_pool"
)

var (
	_ resource.Resource                = &guardRelayPoolAccessResource{}
	_ resource.ResourceWithConfigure   = &guardRelayPoolAccessResource{}
	_ resource.ResourceWithImportState = &guardRelayPoolAccessResource{}
	_ datasource.DataSource            = &guardAllowedRelayPoolsDataSource{}
)

// --- authwise_guard_relay_pool_access ---

// guardRelayPoolAccessResource is authwise_guard_relay_pool_access (tf#66):
// which estate pools one tenant may use, and its default pool. A singleton
// per tenant, written whole (UpdateRelayPoolAccess replaces), so the
// resource is authoritative; destroy writes it empty, leaving the tenant the
// estate default only. Operators only.
type guardRelayPoolAccessResource struct {
	client guardpb.GuardRelayPoolServiceClient
}

type guardRelayPoolAccessModel struct {
	TenantID     types.String `tfsdk:"tenant_id"`
	Name         types.String `tfsdk:"name"`
	AllowedPools types.Set    `tfsdk:"allowed_pools"`
	DefaultPool  types.String `tfsdk:"default_pool"`
}

func newGuardRelayPoolAccessResource() resource.Resource {
	return &guardRelayPoolAccessResource{}
}

func relayPoolAccessName(tenantID string) string {
	return tenantsCollection + "/" + tenantID + "/" + relayPoolAccessID
}

func (r *guardRelayPoolAccessResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardRelayPoolAccessTypeName
}

func (r *guardRelayPoolAccessResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Which estate relay pools a tenant's networks may use, beyond the estate default, and the tenant's default pool. Operators only.\n\n" +
			"One per tenant, and authoritative: pools left out of `allowed_pools` are not allowed. " +
			"A network already on a pool an update removes keeps it, but a write that names it again is refused. " +
			"`default_pool` is the **tenant** default: a network of the tenant created without `relay_pool` gets it, and it is allowed by being the default. " +
			"Empty, the tenant takes the **estate** default, the pool whose `default` is set (`authwise_guard_relay_pool`).\n\n" +
			"Destroy writes the access empty: the tenant keeps the estate default only.",
		Attributes: map[string]schema.Attribute{
			tenantIDAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The kit tenant's id (`t-…`). Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`tenants/t-…/relay-pool-access`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"allowed_pools": schema.SetAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
				MarkdownDescription: "Pools the tenant may use beyond the estate default: `authwise_guard_relay_pool.<name>.name` (`relay-pools/rp-…`). Empty when unset.",
			},
			"default_pool": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "The tenant's default pool, `relay-pools/rp-…`. Empty (the default) means the estate default.",
			},
		},
	}
}

func (r *guardRelayPoolAccessResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = guardRelayPoolClientFrom(req.ProviderData)
}

func (r *guardRelayPoolAccessResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_guard_relay_pool_access is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func (r *guardRelayPoolAccessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *guardRelayPoolAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *guardRelayPoolAccessResource) write(ctx context.Context, from tfsdk.Plan, to *tfsdk.State, diags *diag.Diagnostics) {

	if !r.configured(diags) {
		return
	}

	var plan guardRelayPoolAccessModel
	diags.Append(from.Get(ctx, &plan)...)
	if diags.HasError() {
		return
	}

	var allowed []string
	diags.Append(plan.AllowedPools.ElementsAs(ctx, &allowed, false)...)
	if diags.HasError() {
		return
	}

	name := relayPoolAccessName(plan.TenantID.ValueString())
	out, err := r.client.UpdateRelayPoolAccess(ctx, &guardpb.UpdateRelayPoolAccessRequest{
		RelayPoolAccess: &guardpb.RelayPoolAccess{Name: name, AllowedPools: allowed, DefaultPool: plan.DefaultPool.ValueString()},
	})
	if err != nil {
		diags.AddError("writing "+name+" failed", err.Error())
		return
	}

	fillRelayPoolAccess(ctx, &plan, out, diags)
	diags.Append(to.Set(ctx, &plan)...)
}

func fillRelayPoolAccess(ctx context.Context, m *guardRelayPoolAccessModel, out *guardpb.RelayPoolAccess, diags *diag.Diagnostics) {
	m.Name = types.StringValue(out.GetName())
	allowed, d := types.SetValueFrom(ctx, types.StringType, nonNilSlice(out.GetAllowedPools()))
	diags.Append(d...)
	m.AllowedPools = allowed
	m.DefaultPool = types.StringValue(out.GetDefaultPool())
}

func (r *guardRelayPoolAccessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardRelayPoolAccessModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := relayPoolAccessName(state.TenantID.ValueString())
	out, err := r.client.GetRelayPoolAccess(ctx, &guardpb.GetRelayPoolAccessRequest{Name: name})
	if err != nil {
		if tfruntime.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading "+name+" failed", err.Error())
		return
	}

	fillRelayPoolAccess(ctx, &state, out, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete writes the access empty: the estate default only.
func (r *guardRelayPoolAccessResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardRelayPoolAccessModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := relayPoolAccessName(state.TenantID.ValueString())
	if _, err := r.client.UpdateRelayPoolAccess(ctx, &guardpb.UpdateRelayPoolAccessRequest{
		RelayPoolAccess: &guardpb.RelayPoolAccess{Name: name},
	}); err != nil && !tfruntime.IsNotFound(err) {
		resp.Diagnostics.AddError("clearing "+name+" failed", err.Error())
	}
}

// ImportState takes the kit tenant's id (t-…) or the access's name.
func (r *guardRelayPoolAccessResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	id := strings.TrimSuffix(strings.TrimPrefix(req.ID, tenantsCollection+"/"), "/"+relayPoolAccessID)
	if !strings.HasPrefix(id, "t-") || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("invalid import ID for a tenant's relay pool access",
			"expected the kit tenant's id (t-…) or tenants/t-…/relay-pool-access, got "+req.ID+guardTenantUpgradeHint(id))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), id)...)
}

// --- authwise_guard_allowed_relay_pools ---

// guardAllowedRelayPoolsDataSource is the pools a tenant's networks may
// use, as the tenant sees them (ListAllowedRelayPools): never their relays.
// A tenant's credential reads it; the estate's pools themselves are the
// operators' (authwise_guard_relay_pools).
type guardAllowedRelayPoolsDataSource struct {
	client guardpb.GuardRelayPoolServiceClient
}

var allowedRelayPoolAttrTypes = map[string]attr.Type{
	tfruntime.NameAttribute:      types.StringType,
	"slug":                       types.StringType,
	settingsDisplayNameAttribute: types.StringType,
	"region":                     types.StringType,
	"default":                    types.BoolType,
}

func newGuardAllowedRelayPoolsDataSource() datasource.DataSource {
	return &guardAllowedRelayPoolsDataSource{}
}

func (d *guardAllowedRelayPoolsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardAllowedRelayPoolsTypName
}

func (d *guardAllowedRelayPoolsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		MarkdownDescription: "The relay pools a tenant's networks may use: the estate default, the tenant's default, and the pools an operator allowed it. " +
			"What `authwise_guard_network.relay_pool` takes. Read with the tenant's credential (`guardcontrol.relayPools.listAllowed`); the pools' relays are not shown.",
		Attributes: map[string]dsschema.Attribute{
			tenantIDAttribute: dsschema.StringAttribute{
				Required:            true,
				MarkdownDescription: guardTenantIDDescription,
			},
			"relay_pools": dsschema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The tenant's default first, then by slug.",
				NestedObject: dsschema.NestedAttributeObject{
					Attributes: map[string]dsschema.Attribute{
						tfruntime.NameAttribute:      dsschema.StringAttribute{Computed: true, MarkdownDescription: "`relay-pools/rp-…`: what `relay_pool` takes."},
						"slug":                       dsschema.StringAttribute{Computed: true, MarkdownDescription: "The pool's slug, a name for people; `relay_pool` takes `name`, not this."},
						settingsDisplayNameAttribute: dsschema.StringAttribute{Computed: true},
						"region":                     dsschema.StringAttribute{Computed: true},
						"default":                    dsschema.BoolAttribute{Computed: true, MarkdownDescription: "The tenant's default pool: a network created without `relay_pool` gets it."},
					},
				},
			},
		},
	}
}

func (d *guardAllowedRelayPoolsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = guardRelayPoolClientFrom(req.ProviderData)
}

func (d *guardAllowedRelayPoolsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if d.client == nil {
		resp.Diagnostics.AddError("authwise_guard_allowed_relay_pools is not configured", "Configure was not called with the provider's data")
		return
	}

	var tenantID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(tenantIDAttribute), &tenantID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := d.client.ListAllowedRelayPools(ctx, &guardpb.ListAllowedRelayPoolsRequest{Parent: guardTenantName(tenantID.ValueString())})
	if err != nil {
		resp.Diagnostics.AddError("listing the relay pools "+tenantID.ValueString()+" may use failed", err.Error())
		return
	}

	pools := make([]attr.Value, 0, len(out.GetRelayPools()))
	for _, p := range out.GetRelayPools() {
		v, diags := types.ObjectValue(allowedRelayPoolAttrTypes, map[string]attr.Value{
			tfruntime.NameAttribute:      types.StringValue(p.GetName()),
			"slug":                       types.StringValue(p.GetSlug()),
			settingsDisplayNameAttribute: types.StringValue(p.GetDisplayName()),
			"region":                     types.StringValue(p.GetRegion()),
			"default":                    types.BoolValue(p.GetDefault()),
		})
		resp.Diagnostics.Append(diags...)
		pools = append(pools, v)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: allowedRelayPoolAttrTypes}, pools)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), tenantID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("relay_pools"), list)...)
}

// --- authwise_guard_network: relay_pool's documentation ---

// guardNetworkResource is the generated network with relay_pool and
// pool_fallback described: the generator carries no field descriptions.
// Every other method forwards, the optional interfaces included.
type guardNetworkResource struct {
	inner resource.Resource
}

func newGuardNetworkResource() resource.Resource {
	return &guardNetworkResource{inner: generated.NewNetworkResource()}
}

func (r *guardNetworkResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *guardNetworkResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *guardNetworkResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if inner, ok := r.inner.(resource.ResourceWithValidateConfig); ok {
		inner.ValidateConfig(ctx, req, resp)
	}
}

func (r *guardNetworkResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if inner, ok := r.inner.(resource.ResourceWithModifyPlan); ok {
		inner.ModifyPlan(ctx, req, resp)
	}
}

func (r *guardNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *guardNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *guardNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *guardNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *guardNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

func (r *guardNetworkResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	if a, ok := resp.Schema.Attributes[relayPoolAttribute].(schema.StringAttribute); ok {
		a.MarkdownDescription = "The estate relay pool the network uses: the pool's name, `authwise_guard_relay_pool.<name>.name` or an entry of `authwise_guard_allowed_relay_pools` (`relay-pools/rp-…`). " +
			"guard-control refuses a slug, and a pool the tenant is not allowed (`RELAY_POOL_NOT_ALLOWED`). " +
			"Unset on create, the network gets the tenant's default pool, which is stored: changing a default later never moves a network. " +
			"The network's nodes are served the pool's relays only when it has no relay of its own, or `pool_fallback` is set, and an operator has not stopped its use of the pool (`pool_killed`)."
		resp.Schema.Attributes[relayPoolAttribute] = a
	}
	if a, ok := resp.Schema.Attributes["pool_fallback"].(schema.BoolAttribute); ok {
		a.MarkdownDescription = "Serve the pool after the network's own relays. Default false. guard-control refuses true on a network with no relay of its own (`POOL_FALLBACK_WITHOUT_RELAY`)."
		resp.Schema.Attributes["pool_fallback"] = a
	}
}
