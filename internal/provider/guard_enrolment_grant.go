package provider

import (
	"context"
	"fmt"
	"strings"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// guardTenantAdminClientKey is the ProviderData.Clients key of
// guard-control's GuardTenantAdminService client.
const guardTenantAdminClientKey = "guard_tenant_admin"

// The subject types an enrolment grant names, and the resource type of a
// grant anchored at the Guard tenant itself.
const (
	enrolmentSubjectUser   = "user"
	enrolmentSubjectGroup  = "group"
	enrolmentAnchorTenant  = "guardcontrol.tenant"
	enrolmentAnchorNetwork = "guardcontrol.network"
	enrolmentGrantTypeName = "guard_enrolment_grant"

	networksCollection = "networks"
)

// The roles Guard binds in its audience at the users-link home
// (guard-control#59). guardcontrol.user is enrolment; the others are
// administration, and tenantAdmin is bound only at the Guard tenant,
// networkAdmin only at a network.
const (
	guardRoleUser         = "guardcontrol.user"
	guardRoleTenantAdmin  = "guardcontrol.tenantAdmin"
	guardRoleNetworkAdmin = "guardcontrol.networkAdmin"
	guardRoleViewer       = "guardcontrol.viewer"
)

var (
	_ resource.Resource                   = &guardEnrolmentGrantResource{}
	_ resource.ResourceWithConfigure      = &guardEnrolmentGrantResource{}
	_ resource.ResourceWithImportState    = &guardEnrolmentGrantResource{}
	_ resource.ResourceWithValidateConfig = &guardEnrolmentGrantResource{}
)

// guardEnrolmentGrantResource is authwise_guard_enrolment_grant: one Guard
// role bound to a user or group of a Guard tenant's users link, on the
// users-link home, through guard-control. Its identity is the home's
// binding name. guard-control has no Get for one grant: Read finds it in an
// unpaged list.
//
// The default, guardcontrol.user at the Guard tenant, is an enrolment
// grant (guard-control#55) and keeps the enrolment calls, which a
// guard-control without #59 also serves. Any other role, or any role at a
// network, is a role grant (guard-control#59: GrantRole, RevokeRole,
// ListRoleGrants).
type guardEnrolmentGrantResource struct {
	client guardpb.GuardTenantAdminServiceClient
}

type guardEnrolmentGrantModel struct {
	Binding     types.String `tfsdk:"binding"`
	TenantID    types.String `tfsdk:"tenant_id"`
	NetworkID   types.String `tfsdk:"network_id"`
	Role        types.String `tfsdk:"role"`
	ConditionID types.String `tfsdk:"condition_id"`
	SubjectType types.String `tfsdk:"subject_type"`
	SubjectID   types.String `tfsdk:"subject_id"`
	DisplayName types.String `tfsdk:"display_name"`
	Email       types.String `tfsdk:"email"`
}

func newGuardEnrolmentGrantResource() resource.Resource {
	return &guardEnrolmentGrantResource{}
}

func (r *guardEnrolmentGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + enrolmentGrantTypeName
}

func (r *guardEnrolmentGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Binds one Guard role to a user or group of a Guard tenant's users-link tenant, on the users-link home, through guard-control. " +
			"By default the role is `guardcontrol.user` at the Guard tenant: the subject may enrol devices. " +
			"`guardcontrol.tenantAdmin` (at the Guard tenant), `guardcontrol.networkAdmin` (at a network, `network_id`) and `guardcontrol.viewer` (either) let the subject administer the Guard tenant, or part of it, from the users-link home.\n\n" +
			"**One way, not both.** This is the same binding an `authwise_access_binding` with that role in the Guard audience would write. Declare a grant one way only, or the two will fight.\n\n" +
			"**Who may apply it.** Each call is admitted at guard-control, then by the home on the same bearer, so the provider's credential must administer the users-link tenant. " +
			"A tenant administrator's credential works against a home on identity server 1.46.0 or later (kit#770); on older homes, apply with an operator credential or one holding `access.bindingAdmin` at the tenant. " +
			"A Guard administrator who does not administer the users-link tenant (the MSP shape) is refused with PermissionDenied.\n\n" +
			"**Versions.** A role other than `guardcontrol.user`, or a grant at a network, needs guard-control with role grants (guard-control#59); an older one refuses it with Unimplemented. " +
			"`guardcontrol.tenantAdmin` and `guardcontrol.networkAdmin` carry the home's step-up condition (`condition_id`), and are refused while the home advertises none.\n\n" +
			"**Revoking does not deprovision.** Destroying a grant stops new enrolments; a device already enrolled keeps working until it is revoked by hand or by the deprovisioning sweep (`authwise_guard_deprovisioning`).",
		Attributes: map[string]schema.Attribute{
			"binding": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The binding's name at the users-link home; serves as the Terraform ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			tenantIDAttribute: schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
			},
			networkIDAttribute: schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "A network of the Guard tenant (`n-…`) to bind the role at, instead of the Guard tenant itself. " +
					"Required for `guardcontrol.networkAdmin`; not allowed for `guardcontrol.tenantAdmin`. Changing it replaces the grant.",
				PlanModifiers: replace,
			},
			"role": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(guardRoleUser),
				MarkdownDescription: "`guardcontrol.user` (the default: may enrol), `guardcontrol.tenantAdmin`, `guardcontrol.networkAdmin` or `guardcontrol.viewer`. " +
					"Changing it replaces the grant.",
				// A grant from before role existed has none in state; it is
				// the default, so filling it in replaces nothing.
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(roleChanged,
					"Changing the role replaces the grant.", "Changing the role replaces the grant.")},
				Validators: []validator.String{stringvalidator.OneOf(guardRoleUser, guardRoleTenantAdmin, guardRoleNetworkAdmin, guardRoleViewer)},
			},
			"condition_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The access condition the binding carries: the home's step-up condition on an administrative role; empty on the others.",
			},
			"subject_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`user` or `group`. Changing it replaces the grant.",
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.OneOf(enrolmentSubjectUser, enrolmentSubjectGroup)},
			},
			"subject_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The user's (`u-…`) or group's (`g-…`) id at the home, in one of the Guard tenant's realms; the home refuses any other. " +
					"Changing it replaces the grant.",
				PlanModifiers: replace,
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The subject's display name at the home; empty when the home no longer has it in the Guard tenant's realms.",
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The user's email at the home; empty for a group.",
			},
		},
	}
}

// networkIDIsOptional keeps guardResource from making network_id required:
// a grant without one is bound at the Guard tenant.
func (r *guardEnrolmentGrantResource) networkIDIsOptional() {}

func (r *guardEnrolmentGrantResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = guardTenantAdminClientFrom(req.ProviderData)
}

// ValidateConfig refuses at plan what guard-control or the home would
// refuse at apply: a role at the wrong kind of target, and a subject id of
// the other type.
func (r *guardEnrolmentGrantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var m guardEnrolmentGrantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m.validateTarget(&resp.Diagnostics)

	if m.SubjectType.IsUnknown() || m.SubjectID.IsUnknown() {
		return
	}

	want := map[string]string{enrolmentSubjectUser: "u-", enrolmentSubjectGroup: "g-"}[m.SubjectType.ValueString()]
	if want != "" && !strings.HasPrefix(m.SubjectID.ValueString(), want) {
		resp.Diagnostics.AddAttributeError(path.Root("subject_id"), "subject_id does not match subject_type",
			"a "+m.SubjectType.ValueString()+" grant names a "+want+"… id, got "+m.SubjectID.ValueString())
	}
}

// validateTarget refuses tenantAdmin at a network and networkAdmin at the
// Guard tenant.
func (m *guardEnrolmentGrantModel) validateTarget(diags *diag.Diagnostics) {

	if m.Role.IsUnknown() || m.NetworkID.IsUnknown() {
		return
	}

	switch {
	case m.Role.ValueString() == guardRoleNetworkAdmin && m.NetworkID.IsNull():
		diags.AddAttributeError(path.Root(networkIDAttribute), "guardcontrol.networkAdmin is bound at a network",
			"set network_id to the network the role administers")
	case m.Role.ValueString() == guardRoleTenantAdmin && !m.NetworkID.IsNull():
		diags.AddAttributeError(path.Root(networkIDAttribute), "guardcontrol.tenantAdmin is bound at the Guard tenant",
			"remove network_id, or use guardcontrol.networkAdmin to administer one network")
	}
}

func (r *guardEnrolmentGrantResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_guard_enrolment_grant is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func (r *guardEnrolmentGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan guardEnrolmentGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	subject := &guardpb.EnrolmentSubject{
		Type: plan.SubjectType.ValueString(),
		Id:   plan.SubjectID.ValueString(),
	}

	if plan.isEnrolment() {
		out, err := r.client.GrantEnrolment(ctx, &guardpb.GrantEnrolmentRequest{
			Name:    guardTenantName(plan.TenantID.ValueString()),
			Subject: subject,
		})
		if err != nil {
			addGuardPeopleError(&resp.Diagnostics, "granting enrolment failed", err)
			return
		}
		plan.fromProto(out)
	} else {
		out, err := r.client.GrantRole(ctx, &guardpb.GrantRoleRequest{
			Name:    plan.target(),
			Subject: subject,
			Role:    plan.Role.ValueString(),
		})
		if err != nil {
			addGuardPeopleError(&resp.Diagnostics, "granting "+plan.Role.ValueString()+" failed", err)
			return
		}
		plan.fromRoleProto(out)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *guardEnrolmentGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardEnrolmentGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var found bool
	var err error
	read := "enrolment grants"
	if state.isEnrolment() {
		found, err = r.readEnrolment(ctx, &state)
	} else {
		read = "role grants"
		found, err = r.readRole(ctx, &state)
		// An import has no role yet: from a guard-control without role
		// grants, it can only be an enrolment grant.
		if state.Role.IsNull() && status.Code(err) == codes.Unimplemented {
			read = "enrolment grants"
			found, err = r.readEnrolment(ctx, &state)
		}
	}

	switch {
	case aip.IsNotFound(err) || (err == nil && !found):
		resp.State.RemoveResource(ctx)
	case err != nil:
		addGuardPeopleError(&resp.Diagnostics, "reading "+read+" failed", err)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// readEnrolment finds the grant in ListEnrolmentGrants and fills state from
// it, reporting whether it was there.
func (r *guardEnrolmentGrantResource) readEnrolment(ctx context.Context, state *guardEnrolmentGrantModel) (bool, error) {

	out, err := r.client.ListEnrolmentGrants(ctx, &guardpb.ListEnrolmentGrantsRequest{Name: guardTenantName(state.TenantID.ValueString())})
	if err != nil {
		return false, err
	}

	for _, g := range out.GetGrants() {
		if g.GetBinding() != state.Binding.ValueString() {
			continue
		}
		// Only an import can land here: without role grants, this
		// resource writes enrolment grants at the tenant, never at a
		// network.
		if g.GetResourceType() != enrolmentAnchorTenant {
			return false, fmt.Errorf("binding %s grants enrolment at %s/%s; this guard-control has no role grants (guard-control#59), "+
				"so authwise_guard_enrolment_grant manages grants at the Guard tenant only. Manage it as an authwise_access_binding instead",
				g.GetBinding(), g.GetResourceType(), g.GetResourceId())
		}
		state.fromProto(g)
		return true, nil
	}
	return false, nil
}

// readRole finds the grant in ListRoleGrants and fills state from it,
// reporting whether it was there.
func (r *guardEnrolmentGrantResource) readRole(ctx context.Context, state *guardEnrolmentGrantModel) (bool, error) {

	out, err := r.client.ListRoleGrants(ctx, &guardpb.ListRoleGrantsRequest{Name: guardTenantName(state.TenantID.ValueString())})
	if err != nil {
		return false, err
	}

	for _, g := range out.GetGrants() {
		if g.GetBinding() == state.Binding.ValueString() {
			state.fromRoleProto(g)
			return true, nil
		}
	}
	return false, nil
}

// Update only fills in what a grant from before role and network_id were
// arguments lacks in state: every other change replaces the grant.
func (r *guardEnrolmentGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	var plan guardEnrolmentGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// roleChanged replaces the grant when its role changes, but not when a
// state from before role existed gets its default.
func roleChanged(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.Equal(req.StateValue)
}

// Delete revokes the grant; NotFound counts as already revoked.
func (r *guardEnrolmentGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardEnrolmentGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var err error
	if state.isEnrolment() {
		_, err = r.client.RevokeEnrolment(ctx, &guardpb.RevokeEnrolmentRequest{
			Name:    guardTenantName(state.TenantID.ValueString()),
			Binding: state.Binding.ValueString(),
		})
	} else {
		_, err = r.client.RevokeRole(ctx, &guardpb.RevokeRoleRequest{
			Name:    state.target(),
			Binding: state.Binding.ValueString(),
		})
	}
	if err != nil && !aip.IsNotFound(err) {
		addGuardPeopleError(&resp.Diagnostics, "revoking the grant failed", err)
	}
}

// ImportState takes `<gt-id>/<binding>`: revoking needs the Guard tenant as
// well as the binding, and the binding's name carries slashes of its own.
func (r *guardEnrolmentGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	tenantID, binding, ok := strings.Cut(req.ID, "/")
	if !ok || !strings.HasPrefix(tenantID, "gt-") || binding == "" {
		resp.Diagnostics.AddError("invalid import ID for enrolment grant",
			"expected <guard tenant id>/<binding>, e.g. gt-01/<the binding's name at the home>, got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), tenantID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("binding"), binding)...)
}

// isEnrolment is whether the grant is an enrolment grant: guardcontrol.user
// at the Guard tenant, made and revoked with the enrolment calls. A state
// with no role (an import) is not, until Read finds its role.
func (m *guardEnrolmentGrantModel) isEnrolment() bool {
	return m.Role.ValueString() == guardRoleUser && m.NetworkID.IsNull()
}

// target is the resource name the role is bound at: the Guard tenant or one
// of its networks.
func (m *guardEnrolmentGrantModel) target() string {
	name := guardTenantName(m.TenantID.ValueString())
	if !m.NetworkID.IsNull() {
		name += "/" + networksCollection + "/" + m.NetworkID.ValueString()
	}
	return name
}

func (m *guardEnrolmentGrantModel) fromRoleProto(g *guardpb.RoleGrant) {
	m.Binding = types.StringValue(g.GetBinding())
	m.Role = types.StringValue(g.GetRole())
	m.NetworkID = types.StringNull()
	if g.GetResourceType() == enrolmentAnchorNetwork {
		m.NetworkID = types.StringValue(g.GetResourceId())
	}
	m.ConditionID = types.StringValue(g.GetConditionId())
	m.SubjectType = types.StringValue(g.GetSubject().GetType())
	m.SubjectID = types.StringValue(g.GetSubject().GetId())
	m.DisplayName = types.StringValue(g.GetDisplayName())
	m.Email = types.StringValue(g.GetEmail())
}

func (m *guardEnrolmentGrantModel) fromProto(g *guardpb.EnrolmentGrant) {
	m.Binding = types.StringValue(g.GetBinding())
	m.Role = types.StringValue(guardRoleUser)
	m.ConditionID = types.StringValue("")
	m.SubjectType = types.StringValue(g.GetSubject().GetType())
	m.SubjectID = types.StringValue(g.GetSubject().GetId())
	m.DisplayName = types.StringValue(g.GetDisplayName())
	m.Email = types.StringValue(g.GetEmail())
}

// guardTenantAdminClientFrom is guard-control's tenant admin client, or nil
// before the provider is configured.
func guardTenantAdminClientFrom(providerData any) guardpb.GuardTenantAdminServiceClient {
	pd, ok := providerData.(*tfruntime.ProviderData)
	if !ok {
		return nil
	}
	client, _ := pd.Clients[guardTenantAdminClientKey].(guardpb.GuardTenantAdminServiceClient)
	return client
}

// guardTenantName is a Guard tenant's resource name.
func guardTenantName(id string) string {
	return tenantsCollection + "/" + id
}

// addGuardPeopleError reports a failed call on a Guard tenant's people,
// saying who may make it when it was refused: these calls are admitted at
// guard-control and then by the users-link home.
func addGuardPeopleError(diags *diag.Diagnostics, summary string, err error) {
	detail := err.Error()
	if status.Code(err) == codes.PermissionDenied {
		detail += "\n\nThis call is admitted twice: at guard-control on the Guard tenant, then by the users-link home on the same bearer. " +
			"The provider's credential must administer the users-link tenant. A tenant administrator's credential is admitted by a home on identity server 1.46.0 or later; " +
			"on an older home, use an operator credential or one holding access.bindingAdmin at the tenant."
	}
	diags.AddError(summary, detail)
}
