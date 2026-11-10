package provider

import (
	"context"
	"strings"
	"time"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	guardTenantSettingsTypeName = "guard_tenant_settings"
	audienceAttribute           = "audience"

	settingsDisplayNameAttribute = "display_name"
	settingsEnabledAttribute     = "enabled"
)

// objectAsOptions reads a nested object strictly: no unknown or null is
// tolerated where the model has a plain field.
var objectAsOptions = basetypes.ObjectAsOptions{}

// The update mask paths of Guard's settings for a tenant (apis v0.57.0).
// There is no slug path: the slug is kit's Tenant.slug, and guard-control
// refuses one (SLUG_MANAGED_BY_IDENTITY).
const (
	maskLabels                 = "labels"
	maskUsersRealms            = "config.users.realms"
	maskDeprovisioningEnabled  = "config.deprovisioning.enabled"
	guardTenantSettingsSummary = "Guard's settings for a kit tenant"
)

var (
	_ resource.Resource                = &guardTenantSettingsResource{}
	_ resource.ResourceWithConfigure   = &guardTenantSettingsResource{}
	_ resource.ResourceWithImportState = &guardTenantSettingsResource{}
)

// guardTenantSettingsResource is authwise_guard_tenant_settings (tf#65):
// Guard's settings for a kit tenant, on guard-control's kept Tenant RPCs.
// There is no Guard tenant since the collapse (apis v0.57.0); guard-control
// keys its rows by kit's t-…, and PatchTenant is an upsert once kit reports
// Guard on the tenant. So create and update are both PatchTenant, and
// destroy writes nothing: DeleteTenant is operator-only and refused while
// networks or users remain, which is no Terraform destroy.
//
// Every writable attribute is optional and computed: left unset, it is
// neither written nor reset, and reads back what guard-control holds. Each
// write's mask names only what the plan changes.
type guardTenantSettingsResource struct {
	client guardpb.GuardControlServiceClient
}

type guardTenantSettingsModel struct {
	TenantID        types.String `tfsdk:"tenant_id"`
	Name            types.String `tfsdk:"name"`
	Slug            types.String `tfsdk:"slug"`
	Labels          types.Map    `tfsdk:"labels"`
	Config          types.Object `tfsdk:"config"`
	DisplayName     types.String `tfsdk:"display_name"`
	Status          types.String `tfsdk:"status"`
	Issuer          types.String `tfsdk:"issuer"`
	Audience        types.String `tfsdk:"audience"`
	ConsoleClientID types.String `tfsdk:"console_client_id"`
	Plan            types.Object `tfsdk:"plan"`
}

type guardTenantConfigModel struct {
	Users          types.Object `tfsdk:"users"`
	Deprovisioning types.Object `tfsdk:"deprovisioning"`
}

type guardTenantUsersModel struct {
	Realms types.List `tfsdk:"realms"`
}

type guardTenantDeprovisioningModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
}

var (
	guardTenantUsersAttrTypes          = map[string]attr.Type{"realms": types.ListType{ElemType: types.StringType}}
	guardTenantDeprovisioningAttrTypes = map[string]attr.Type{settingsEnabledAttribute: types.BoolType}
	guardTenantConfigAttrTypes         = map[string]attr.Type{
		"users":          types.ObjectType{AttrTypes: guardTenantUsersAttrTypes},
		"deprovisioning": types.ObjectType{AttrTypes: guardTenantDeprovisioningAttrTypes},
	}
)

func newGuardTenantSettingsResource() resource.Resource {
	return &guardTenantSettingsResource{}
}

func (r *guardTenantSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardTenantSettingsTypeName
}

func (r *guardTenantSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: guardTenantSettingsSummary + ": its labels, the realms its people come from, and whether its devices are swept. " +
			"Guard has no tenant of its own: its networks and settings are keyed by the kit tenant (`t-…`), and what kit owns about the tenant (its name, slug, status, and the issuer, audience and console client its people sign in through) is read-only here.\n\n" +
			"guard-control writes the settings once kit reports Guard on the tenant; until then it refuses with `GUARD_NOT_ENABLED`, and a suspended tenant with `TENANT_SUSPENDED`. " +
			"Each refusal is guard-control's own message.\n\n" +
			"An attribute left unset is neither written nor reset: it reads back what guard-control holds. " +
			"Removing one from the configuration later leaves its last value in place; set it explicitly (`realms = []`, `enabled = false`) to change it.\n\n" +
			"**Destroy writes nothing.** It removes the resource from state and leaves Guard's settings on the tenant. Deleting a tenant from Guard is an operator's task, refused while it has networks or users.",
		Attributes: map[string]schema.Attribute{
			tenantIDAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The kit tenant's id (`t-…`). Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The tenant's resource name at guard-control, `tenants/t-…`.",
				PlanModifiers:       keep,
			},
			"slug": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The kit tenant's slug, which names it in mesh DNS (`<node>.<network>.<slug>.guard.<estate>`). " +
					"It is kit's, set on the tenant in kit (`awtenant tenants update --slug`, kit 1.83.0 or later); guard-control refuses to write it (`SLUG_MANAGED_BY_IDENTITY`).",
				PlanModifiers: keep,
			},
			labelsAttribute: schema.MapAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Guard's labels on the tenant. Written whole when it changes.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			configAttribute: schema.SingleNestedAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Guard's settings for the tenant.",
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"users": schema.SingleNestedAttribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Where the tenant's people come from.",
						PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
						Attributes: map[string]schema.Attribute{
							"realms": schema.ListAttribute{
								Optional:            true,
								Computed:            true,
								ElementType:         types.StringType,
								MarkdownDescription: "The realm ids (`r-…`) whose people are the tenant's. Empty means every realm the tenant's issuer routes to.",
								PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
							},
						},
					},
					"deprovisioning": schema.SingleNestedAttribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "The deprovisioning sweep.",
						PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
						Attributes: map[string]schema.Attribute{
							settingsEnabledAttribute: schema.BoolAttribute{
								Optional: true,
								Computed: true,
								MarkdownDescription: "Sweep the tenant's devices: a device a person enrolled keeps working after they lose Guard, because its token refreshes on the node's own identity. " +
									"The sweep re-checks with guard-control's install credential and revokes each node whose owner no binding reaches any more. It never revokes on a failed read.",
								PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
							},
						},
					},
				},
			},
			settingsDisplayNameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The kit tenant's display name.",
				PlanModifiers:       keep,
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The kit tenant's state (`ACTIVE`, `SUSPENDED`). Suspension is kit's.",
			},
			"issuer": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The issuer the tenant's people sign in at, exactly as their tokens' `iss`.",
				PlanModifiers:       keep,
			},
			audienceAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The Guard audience the tenant's people's tokens carry (`aud`).",
				PlanModifiers:       keep,
			},
			"console_client_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The Guard console's public client on the tenant's issuer.",
				PlanModifiers:       keep,
			},
			"plan": schema.SingleNestedAttribute{
				Computed: true,
				MarkdownDescription: "The tenant's plan and its caps, read from kit's tenant (guard-control#88). guard-control enforces `devices_limit` and `relay_bytes_limit` " +
					"(a network over its relay cap reads `pool_capped`) and treats an expired trial as read-only; kit enforces `members_limit` at its invitation. 0 is no cap.",
				Attributes: map[string]schema.Attribute{
					"plan":              schema.StringAttribute{Computed: true, MarkdownDescription: "`TRIAL`, `FREE`, `TEAM`, `BUSINESS` or `ENTERPRISE`; empty when kit reports none."},
					"trial_ends_at":     schema.StringAttribute{Computed: true, MarkdownDescription: "When a trial ends, RFC 3339; empty otherwise."},
					"members_limit":     schema.Int64Attribute{Computed: true, MarkdownDescription: "The most people the tenant may have; 0 is no cap."},
					"devices_limit":     schema.Int64Attribute{Computed: true, MarkdownDescription: "The most devices the tenant may enroll; 0 is no cap."},
					"relay_bytes_limit": schema.Int64Attribute{Computed: true, MarkdownDescription: "The relay traffic the tenant may use, in bytes; 0 is no cap."},
				},
			},
		},
	}
}

func (r *guardTenantSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = guardClientFrom(req.ProviderData)
}

func (r *guardTenantSettingsResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_guard_tenant_settings is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func (r *guardTenantSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan guardTenantSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.write(ctx, &plan, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *guardTenantSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state guardTenantSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.write(ctx, &plan, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// write patches what the plan sets and prior (nil on create) does not
// already hold, or reads the settings when nothing changed, and fills plan
// from guard-control's answer.
func (r *guardTenantSettingsResource) write(ctx context.Context, plan, prior *guardTenantSettingsModel, diags *diag.Diagnostics) {

	name := guardTenantName(plan.TenantID.ValueString())
	tenant, paths := guardTenantPatch(ctx, plan, prior, diags)
	if diags.HasError() {
		return
	}
	tenant.Name = name

	var out *guardpb.Tenant
	var err error
	if len(paths) == 0 {
		out, err = r.client.GetTenant(ctx, &guardpb.GetTenantRequest{Name: name})
	} else {
		out, err = r.client.PatchTenant(ctx, &guardpb.PatchTenantRequest{Tenant: tenant, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths}})
	}
	if err != nil {
		diags.AddError("writing Guard's settings for "+plan.TenantID.ValueString()+" failed", err.Error())
		return
	}

	fillGuardTenantSettings(ctx, plan, out, diags)
}

// guardTenantPatch is the tenant to patch and its mask: each value the plan
// sets that prior (nil on create) does not already hold.
func guardTenantPatch(ctx context.Context, plan, prior *guardTenantSettingsModel, diags *diag.Diagnostics) (*guardpb.Tenant, []string) {

	tenant := &guardpb.Tenant{Config: &guardpb.TenantConfig{
		Users:          &guardpb.TenantUsersConfig{},
		Deprovisioning: &guardpb.TenantDeprovisioningConfig{},
	}}
	var paths []string

	held := guardTenantSettingsModel{Labels: types.MapNull(types.StringType), Config: types.ObjectNull(guardTenantConfigAttrTypes)}
	if prior != nil {
		held = *prior
	}
	changed := func(planned, was attr.Value) bool {
		return !planned.IsNull() && !planned.IsUnknown() && (prior == nil || !planned.Equal(was))
	}

	heldRealms, heldEnabled := settingsLeaves(ctx, held.Config, diags)
	realms, enabled := settingsLeaves(ctx, plan.Config, diags)

	if changed(plan.Labels, held.Labels) {
		diags.Append(plan.Labels.ElementsAs(ctx, &tenant.Labels, false)...)
		paths = append(paths, maskLabels)
	}
	if changed(realms, heldRealms) {
		diags.Append(realms.ElementsAs(ctx, &tenant.Config.Users.Realms, false)...)
		paths = append(paths, maskUsersRealms)
	}
	if changed(enabled, heldEnabled) {
		tenant.Config.Deprovisioning.Enabled = enabled.ValueBool()
		paths = append(paths, maskDeprovisioningEnabled)
	}
	return tenant, paths
}

// settingsLeaves is config's realms and enabled, unknown where config, or
// the object holding them, is.
func settingsLeaves(ctx context.Context, config types.Object, diags *diag.Diagnostics) (types.List, types.Bool) {

	realms := types.ListUnknown(types.StringType)
	enabled := types.BoolUnknown()
	if config.IsNull() || config.IsUnknown() {
		return realms, enabled
	}

	var c guardTenantConfigModel
	diags.Append(config.As(ctx, &c, objectAsOptions)...)
	if !c.Users.IsNull() && !c.Users.IsUnknown() {
		var u guardTenantUsersModel
		diags.Append(c.Users.As(ctx, &u, objectAsOptions)...)
		realms = u.Realms
	}
	if !c.Deprovisioning.IsNull() && !c.Deprovisioning.IsUnknown() {
		var d guardTenantDeprovisioningModel
		diags.Append(c.Deprovisioning.As(ctx, &d, objectAsOptions)...)
		enabled = d.Enabled
	}
	return realms, enabled
}

// fillGuardTenantSettings sets m from guard-control's tenant.
func fillGuardTenantSettings(ctx context.Context, m *guardTenantSettingsModel, t *guardpb.Tenant, diags *diag.Diagnostics) {

	m.Name = types.StringValue(t.GetName())
	m.TenantID = types.StringValue(strings.TrimPrefix(t.GetName(), tenantsCollection+"/"))
	m.Slug = types.StringValue(t.GetSlug())
	m.DisplayName = types.StringValue(t.GetDisplayName())
	m.Status = types.StringValue(t.GetStatus())
	m.Issuer = types.StringValue(t.GetIssuer())
	m.Audience = types.StringValue(t.GetAudience())
	m.ConsoleClientID = types.StringValue(t.GetConsoleClientId())

	labels, d := types.MapValueFrom(ctx, types.StringType, nonNilMap(t.GetLabels()))
	diags.Append(d...)
	m.Labels = labels

	realms, d := types.ListValueFrom(ctx, types.StringType, nonNilSlice(t.GetConfig().GetUsers().GetRealms()))
	diags.Append(d...)
	users, d := types.ObjectValue(guardTenantUsersAttrTypes, map[string]attr.Value{"realms": realms})
	diags.Append(d...)
	deprovisioning, d := types.ObjectValueFrom(ctx, guardTenantDeprovisioningAttrTypes, guardTenantDeprovisioningModel{
		Enabled: types.BoolValue(t.GetConfig().GetDeprovisioning().GetEnabled()),
	})
	diags.Append(d...)
	config, d := types.ObjectValue(guardTenantConfigAttrTypes, map[string]attr.Value{"users": users, "deprovisioning": deprovisioning})
	diags.Append(d...)
	m.Config = config

	p := t.GetPlan()
	trialEnds := ""
	if p.GetTrialEndsAt() != nil {
		trialEnds = p.GetTrialEndsAt().AsTime().UTC().Format(time.RFC3339)
	}
	plan, d := types.ObjectValue(guardTenantPlanAttrTypes, map[string]attr.Value{
		"plan":              types.StringValue(p.GetPlan()),
		"trial_ends_at":     types.StringValue(trialEnds),
		"members_limit":     types.Int64Value(p.GetMembersLimit()),
		"devices_limit":     types.Int64Value(p.GetDevicesLimit()),
		"relay_bytes_limit": types.Int64Value(p.GetRelayBytesLimit()),
	})
	diags.Append(d...)
	m.Plan = plan
}

// guardTenantPlanAttrTypes are the plan object's attributes.
var guardTenantPlanAttrTypes = map[string]attr.Type{
	"plan": types.StringType, "trial_ends_at": types.StringType,
	"members_limit": types.Int64Type, "devices_limit": types.Int64Type, "relay_bytes_limit": types.Int64Type,
}

// Read reads the settings back, so a change made elsewhere shows as drift.
// A tenant guard-control no longer knows is gone from state.
func (r *guardTenantSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardTenantSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetTenant(ctx, &guardpb.GetTenantRequest{Name: guardTenantName(state.TenantID.ValueString())})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading Guard's settings for "+state.TenantID.ValueString()+" failed", err.Error())
		return
	}

	fillGuardTenantSettings(ctx, &state, out, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete writes nothing: the settings stay on the tenant.
func (r *guardTenantSettingsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

// ImportState takes the kit tenant's id (t-…) or name (tenants/t-…).
func (r *guardTenantSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	id := strings.TrimPrefix(req.ID, tenantsCollection+"/")
	if !strings.HasPrefix(id, "t-") || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("invalid import ID for Guard's tenant settings",
			"expected the kit tenant's id (t-…) or its full name (tenants/t-…), got "+req.ID+guardTenantUpgradeHint(id))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), id)...)
}

// guardClientFrom is the GuardControlService client in the provider's data,
// or nil before the provider is configured.
func guardClientFrom(providerData any) guardpb.GuardControlServiceClient {
	pd, ok := providerData.(*tfruntime.ProviderData)
	if !ok {
		return nil
	}
	client, _ := pd.Clients[guardClientKey].(guardpb.GuardControlServiceClient)
	return client
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
