package provider

import (
	"context"
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
	enrolmentGrantTypeName = "guard_enrolment_grant"
)

var (
	_ resource.Resource                   = &guardEnrolmentGrantResource{}
	_ resource.ResourceWithConfigure      = &guardEnrolmentGrantResource{}
	_ resource.ResourceWithImportState    = &guardEnrolmentGrantResource{}
	_ resource.ResourceWithValidateConfig = &guardEnrolmentGrantResource{}
)

// guardEnrolmentGrantResource is authwise_guard_enrolment_grant: one user or
// group of a Guard tenant's users link that may enrol (guard-control#55).
// The grant is a guardcontrol.user binding on the users-link home, written
// through guard-control, which fixes the role and the resource; so the
// subject is all the resource takes. Its identity is the home's binding
// name. guard-control has no Get for one grant: Read finds it in
// ListEnrolmentGrants, which is unpaged.
type guardEnrolmentGrantResource struct {
	client guardpb.GuardTenantAdminServiceClient
}

type guardEnrolmentGrantModel struct {
	Binding     types.String `tfsdk:"binding"`
	TenantID    types.String `tfsdk:"tenant_id"`
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
		MarkdownDescription: "Lets one user or group of a Guard tenant's users-link tenant enrol devices: a `guardcontrol.user` binding at the Guard tenant, " +
			"written on the users-link home through guard-control, which fixes the role and the resource.\n\n" +
			"**One way, not both.** This is the same binding an `authwise_access_binding` with role `guardcontrol.user` in the Guard audience would write. Declare a grant one way only, or the two will fight.\n\n" +
			"**Who may apply it.** Each call is admitted at guard-control, then by the home on the same bearer, so the provider's credential must administer the users-link tenant. " +
			"A tenant administrator's credential works against a home on identity server 1.46.0 or later (kit#770); on older homes, apply with an operator credential or one holding `access.bindingAdmin` at the tenant. " +
			"A Guard administrator who does not administer the users-link tenant (the MSP shape) is refused with PermissionDenied.\n\n" +
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

func (r *guardEnrolmentGrantResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = guardTenantAdminClientFrom(req.ProviderData)
}

// ValidateConfig refuses a subject id of the other type at plan; the home
// would refuse it at apply.
func (r *guardEnrolmentGrantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var m guardEnrolmentGrantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.SubjectType.IsUnknown() || m.SubjectID.IsUnknown() {
		return
	}

	want := map[string]string{enrolmentSubjectUser: "u-", enrolmentSubjectGroup: "g-"}[m.SubjectType.ValueString()]
	if want != "" && !strings.HasPrefix(m.SubjectID.ValueString(), want) {
		resp.Diagnostics.AddAttributeError(path.Root("subject_id"), "subject_id does not match subject_type",
			"a "+m.SubjectType.ValueString()+" grant names a "+want+"… id, got "+m.SubjectID.ValueString())
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

	out, err := r.client.GrantEnrolment(ctx, &guardpb.GrantEnrolmentRequest{
		Name: guardTenantName(plan.TenantID.ValueString()),
		Subject: &guardpb.EnrolmentSubject{
			Type: plan.SubjectType.ValueString(),
			Id:   plan.SubjectID.ValueString(),
		},
	})
	if err != nil {
		addGuardPeopleError(&resp.Diagnostics, "granting enrolment failed", err)
		return
	}

	plan.fromProto(out)
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

	out, err := r.client.ListEnrolmentGrants(ctx, &guardpb.ListEnrolmentGrantsRequest{Name: guardTenantName(state.TenantID.ValueString())})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addGuardPeopleError(&resp.Diagnostics, "reading enrolment grants failed", err)
		return
	}

	for _, g := range out.GetGrants() {
		if g.GetBinding() != state.Binding.ValueString() {
			continue
		}
		// Only an import can land here: this resource writes grants at
		// the tenant, never at a network.
		if g.GetResourceType() != enrolmentAnchorTenant {
			resp.Diagnostics.AddError("the grant is anchored at a network",
				"binding "+g.GetBinding()+" grants enrolment at "+g.GetResourceType()+"/"+g.GetResourceId()+
					"; authwise_guard_enrolment_grant manages grants at the Guard tenant only. Manage it as an authwise_access_binding instead.")
			return
		}
		state.fromProto(g)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	resp.State.RemoveResource(ctx)
}

// Update is never called: every argument replaces the grant.
func (r *guardEnrolmentGrantResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("enrolment grants cannot be updated", "every argument of authwise_guard_enrolment_grant replaces it")
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

	_, err := r.client.RevokeEnrolment(ctx, &guardpb.RevokeEnrolmentRequest{
		Name:    guardTenantName(state.TenantID.ValueString()),
		Binding: state.Binding.ValueString(),
	})
	if err != nil && !aip.IsNotFound(err) {
		addGuardPeopleError(&resp.Diagnostics, "revoking enrolment failed", err)
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

func (m *guardEnrolmentGrantModel) fromProto(g *guardpb.EnrolmentGrant) {
	m.Binding = types.StringValue(g.GetBinding())
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
