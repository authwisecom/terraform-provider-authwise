package provider

import (
	"context"
	"time"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	// guardEnrollmentKeyClientKey is GuardEnrollmentKeyService
	// (guard-control#98).
	guardEnrollmentKeyClientKey = "guard_enrollment_key"

	guardEnrollmentKeyTypeName = "guard_enrollment_key"
	enrollmentKeysCollection   = "enrollmentKeys"

	attrMaxUses           = "max_uses"
	attrEphemeral         = "ephemeral"
	attrRecreateIfInvalid = "recreate_if_invalid"

	// maxEnrollmentKeyUses is guard-control's ceiling, which stands for
	// "unlimited" (EK-D9).
	maxEnrollmentKeyUses = 1000
	maxExpiryDays        = 90
)

var (
	_ resource.Resource                   = &guardEnrollmentKeyResource{}
	_ resource.ResourceWithConfigure      = &guardEnrollmentKeyResource{}
	_ resource.ResourceWithValidateConfig = &guardEnrollmentKeyResource{}
)

// guardEnrollmentKeyResource is authwise_guard_enrollment_key (tf#74; train
// 2026.11.GFK.1 adds node_group): a secret that lets machines join one
// network as server nodes with no person signing in.
//
// The rulings of 2026-11-11 (kit docswip/GUARD_CONNECT.md §6): the key is
// created, read and revoked, never updated, so every argument replaces.
// The secret is shown once and kept in state. expiry_days in, expires_at
// out. Read drops a revoked or deleted key, and an expired or used-up one
// unless recreate_if_invalid is false. Destroy revokes, NotFound and
// already-revoked counting as success, and enrolled nodes stay. No import:
// the secret is never readable again.
type guardEnrollmentKeyResource struct {
	client guardpb.GuardEnrollmentKeyServiceClient
}

type guardEnrollmentKeyModel struct {
	TenantID          types.String `tfsdk:"tenant_id"`
	NetworkID         types.String `tfsdk:"network_id"`
	ID                types.String `tfsdk:"guard_enrollment_key_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	ExpiryDays        types.Int64  `tfsdk:"expiry_days"`
	MaxUses           types.Int64  `tfsdk:"max_uses"`
	Ephemeral         types.Bool   `tfsdk:"ephemeral"`
	NodeNamePrefix    types.String `tfsdk:"node_name_prefix"`
	NodeGroup         types.String `tfsdk:"node_group"`
	RecreateIfInvalid types.Bool   `tfsdk:"recreate_if_invalid"`
	Secret            types.String `tfsdk:"secret"`
	ExpiresAt         types.String `tfsdk:"expires_at"`
	Uses              types.Int64  `tfsdk:"uses"`
	LastUsedAt        types.String `tfsdk:"last_used_at"`
	State             types.String `tfsdk:"state"`
	CreatedBy         types.String `tfsdk:"created_by"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

func newGuardEnrollmentKeyResource() resource.Resource {
	return &guardEnrollmentKeyResource{}
}

func (r *guardEnrollmentKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardEnrollmentKeyTypeName
}

func (r *guardEnrollmentKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A Guard enrollment key: a secret that lets machines join one network as server nodes, with no person signing in and no node registered first. " +
			"Hand `secret` to the machine (`awguard up --enrollment-key`, or a Secret the Guard operator reads in a Kubernetes cluster).\n\n" +
			"guard-control shows the secret once, so it is kept in state: **treat the state as sensitive.** " +
			"A key is never changed: every argument replaces it, which revokes the old key and creates a new one with a new secret.\n\n" +
			"A key that is revoked or gone drops out of state at refresh, and the next apply creates a new one. So does a key that has expired or is used up, unless `recreate_if_invalid = false`. " +
			"**Destroy revokes the key.** The nodes it enrolled stay; remove them separately if they should go too.\n\n" +
			"`node_group` adds every node the key enrolls to that group at join, so a machine that joins is reachable and served at once.\n\n" +
			"There is no import: the secret is never readable again.",
		Attributes: map[string]schema.Attribute{
			tenantIDAttribute: schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
			},
			networkIDAttribute: schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
			},
			"guard_enrollment_key_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The key's id, `ek-…`.",
				PlanModifiers:       keep,
			},
			attrName: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The key's full name, `tenants/t-…/networks/n-…/enrollmentKeys/ek-…`. A node's `enrollment_key` names it.",
				PlanModifiers:       keep,
			},
			attrDescription: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "What the key is for, shown in the console.",
				PlanModifiers:       replace,
			},
			"expiry_days": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Days from creation until the key stops working: 1 to 90. Unset is 30.",
				Validators:          []validator.Int64{int64validator.Between(1, maxExpiryDays)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			attrMaxUses: schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "How many machines may join with the key: 1 to 1000. Unset is 1. " +
					"There is no unlimited key: 1000 is the ceiling and stands for it.",
				Validators:    []validator.Int64{int64validator.Between(1, maxEnrollmentKeyUses)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace(), int64planmodifier.UseStateForUnknown()},
			},
			attrEphemeral: schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Remove a node the key enrolled once it has been offline an hour: for autoscaled and short-lived machines. " +
					"Refused on a one-use key (`max_uses` unset or 1).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"node_name_prefix": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Name a node `<prefix>-<host name>`, with a number added when the name is taken: a DNS label of at most 32 characters, not `nd`. " +
					"Unset, a node is named after its host.",
				PlanModifiers: replace,
			},
			"node_group": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "A node group of the network, `authwise_guard_node_group.<name>.name`, that every node the key enrolls joins. " +
					"While the key is unrevoked, the group cannot be deleted.",
				PlanModifiers: replace,
			},
			attrRecreateIfInvalid: schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Drop an expired or used-up key from state at refresh, so that the next apply creates a new one. Default true. " +
					"With false, such a key stays in state as it is (`state` says why), until it is replaced by hand. A revoked key is always dropped.",
			},
			"secret": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "The key, `gk_…`.",
				PlanModifiers:       keep,
			},
			"expires_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key stops working, RFC 3339.",
				PlanModifiers:       keep,
			},
			"uses": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "How many machines have joined with the key.",
			},
			"last_used_at": schema.StringAttribute{
				Computed: true,
			},
			"state": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`ACTIVE`, `EXPIRED` or `USED_UP`. A revoked key is not kept in state.",
			},
			"created_by": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: keep,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: keep,
			},
		},
	}
}

func (r *guardEnrollmentKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.client, _ = pd.Clients[guardEnrollmentKeyClientKey].(guardpb.GuardEnrollmentKeyServiceClient)
	}
}

func (r *guardEnrollmentKeyResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_guard_enrollment_key is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

// ValidateConfig refuses ephemeral on a one-use key (EK-D10).
func (r *guardEnrollmentKeyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var ephemeral types.Bool
	var maxUses types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrEphemeral), &ephemeral)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrMaxUses), &maxUses)...)
	if resp.Diagnostics.HasError() || ephemeral.IsUnknown() || maxUses.IsUnknown() || !ephemeral.ValueBool() {
		return
	}
	if maxUses.IsNull() || maxUses.ValueInt64() == 1 {
		resp.Diagnostics.AddAttributeError(path.Root(attrEphemeral), "a one-use key cannot be ephemeral",
			"An ephemeral key is for autoscaled machines that come and go: set max_uses above 1, or leave ephemeral out.")
	}
}

func (m *guardEnrollmentKeyModel) fromProto(k *guardpb.EnrollmentKey) {
	m.Name = types.StringValue(k.GetName())
	if ids, id, err := guardNetworkScope.ParseName(enrollmentKeysCollection, k.GetName()); err == nil {
		m.TenantID = types.StringValue(ids[tenantIDAttribute])
		m.NetworkID = types.StringValue(ids[networkIDAttribute])
		m.ID = types.StringValue(id)
	}
	m.MaxUses = types.Int64Value(int64(k.GetMaxUses()))
	m.Ephemeral = types.BoolValue(k.GetEphemeral())
	m.ExpiresAt = timestampString(k.GetExpiresAt())
	m.Uses = types.Int64Value(int64(k.GetUses()))
	m.LastUsedAt = timestampString(k.GetLastUsedAt())
	m.State = types.StringValue(enrollmentKeyState(k.GetState()))
	m.CreatedBy = types.StringValue(k.GetCreatedBy())
	m.CreatedAt = timestampString(k.GetCreatedAt())
}

// enrollmentKeyState is the state without its enum prefix.
func enrollmentKeyState(s guardpb.EnrollmentKeyState) string {
	const prefix = "ENROLLMENT_KEY_STATE_"
	name := s.String()
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		return name[len(prefix):]
	}
	return name
}

func (r *guardEnrollmentKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan guardEnrollmentKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parent, err := guardNetworkScope.ComposeParent(map[string]string{
		tenantIDAttribute:  plan.TenantID.ValueString(),
		networkIDAttribute: plan.NetworkID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("cannot name the network", err.Error())
		return
	}

	key := &guardpb.EnrollmentKey{
		Description:    plan.Description.ValueString(),
		Ephemeral:      plan.Ephemeral.ValueBool(),
		NodeNamePrefix: plan.NodeNamePrefix.ValueString(),
		NodeGroup:      plan.NodeGroup.ValueString(),
	}
	if !plan.MaxUses.IsNull() && !plan.MaxUses.IsUnknown() {
		key.MaxUses = int32(plan.MaxUses.ValueInt64()) //nolint:gosec // validated 1 to 1000
	}
	if !plan.ExpiryDays.IsNull() {
		key.Ttl = durationpb.New(time.Duration(plan.ExpiryDays.ValueInt64()) * 24 * time.Hour)
	}

	out, err := r.client.CreateEnrollmentKey(ctx, &guardpb.CreateEnrollmentKeyRequest{Parent: parent, EnrollmentKey: key})
	if err != nil {
		resp.Diagnostics.AddError("creating the enrollment key failed", err.Error())
		return
	}

	plan.fromProto(out)
	plan.Secret = types.StringValue(out.GetSecret())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read keeps the secret, which no read returns, and drops a key that no
// longer admits a join (see the type's comment).
func (r *guardEnrollmentKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardEnrollmentKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetEnrollmentKey(ctx, &guardpb.GetEnrollmentKeyRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading enrollment key "+state.Name.ValueString()+" failed", err.Error())
		return
	}

	if keyDropped(out, state.RecreateIfInvalid) {
		resp.State.RemoveResource(ctx)
		return
	}

	state.fromProto(out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// keyDropped is whether refresh drops a key: revoked always, expired or
// used up unless recreate_if_invalid is false.
func keyDropped(k *guardpb.EnrollmentKey, recreateIfInvalid types.Bool) bool {
	switch k.GetState() {
	case guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_REVOKED:
		return true
	case guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_EXPIRED, guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_USED_UP:
		return recreateIfInvalid.IsNull() || recreateIfInvalid.ValueBool()
	case guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_UNSPECIFIED, guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_ACTIVE:
	}
	return k.GetRevokedAt() != nil
}

// Update writes only recreate_if_invalid, which is the provider's own:
// every argument guard-control holds replaces.
func (r *guardEnrollmentKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state guardEnrollmentKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.RecreateIfInvalid = plan.RecreateIfInvalid
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete revokes. NotFound counts as revoked, and revoking a revoked key
// returns it unchanged.
func (r *guardEnrollmentKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardEnrollmentKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.RevokeEnrollmentKey(ctx, &guardpb.RevokeEnrollmentKeyRequest{Name: state.Name.ValueString()}); err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("revoking enrollment key "+state.Name.ValueString()+" failed", err.Error())
	}
}
