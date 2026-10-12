package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service accounts (kit#756, #37) are clients of the admin issuer that a
// tenant owns, reached through the public tenancy service on kit's
// endpoint, at central only. Both resources are hand-written: the
// account's secrets are a repeated message, which tfinfra does not
// generate, and a secret has no read or delete of its own.

const (
	// tenancyClientKey is the ProviderData.Clients key of the tenancy
	// client.
	tenancyClientKey = "tenancy"

	serviceAccountCollection = "service-accounts"

	createdAtAttribute = "created_at"
	expiresAtAttribute = "expires_at"
)

var serviceAccountScope = aip.NewScope("tenants")

var (
	_ resource.Resource                = &serviceAccountResource{}
	_ resource.ResourceWithConfigure   = &serviceAccountResource{}
	_ resource.ResourceWithImportState = &serviceAccountResource{}
	_ resource.Resource                = &serviceAccountSecretResource{}
	_ resource.ResourceWithConfigure   = &serviceAccountSecretResource{}
)

func tenancyClientFrom(providerData any, diags *diag.Diagnostics) tenancypb.AuthwiseTenancyServiceClient {

	if providerData == nil {
		return nil
	}

	pd, ok := providerData.(*tfruntime.ProviderData)
	if !ok {
		diags.AddError("unexpected provider data", fmt.Sprintf("expected *tf.ProviderData, got %T", providerData))
		return nil
	}

	client, ok := pd.Clients[tenancyClientKey].(tenancypb.AuthwiseTenancyServiceClient)
	if !ok {
		diags.AddError("missing client", `provider data key "tenancy" is not an AuthwiseTenancyServiceClient`)
		return nil
	}

	return client
}

// parseTimestamp reads an optional RFC 3339 attribute; nil when unset.
func parseTimestamp(v types.String, at path.Path, diags *diag.Diagnostics) *timestamppb.Timestamp {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v.ValueString())
	if err != nil {
		diags.AddAttributeError(at, "not an RFC 3339 timestamp", err.Error())
		return nil
	}
	return timestamppb.New(t)
}

func timestampString(ts *timestamppb.Timestamp) types.String {
	if ts == nil {
		return types.StringNull()
	}
	return types.StringValue(ts.AsTime().UTC().Format(time.RFC3339))
}

// --- authwise_service_account ---

type serviceAccountResource struct {
	client   tenancypb.AuthwiseTenancyServiceClient
	defaults map[string]string
}

type serviceAccountModel struct {
	Name        types.String `tfsdk:"name"`
	TenantID    types.String `tfsdk:"tenant_id"`
	DisplayName types.String `tfsdk:"display_name"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
	Roles       types.Set    `tfsdk:"roles"`
	ClientID    types.String `tfsdk:"client_id"`
	Status      types.String `tfsdk:"status"`
	Secrets     types.List   `tfsdk:"secrets"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func serviceAccountSecretAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"id": types.StringType, createdAtAttribute: types.StringType, expiresAtAttribute: types.StringType}
}

func newServiceAccountResource() resource.Resource {
	return &serviceAccountResource{}
}

func (r *serviceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (r *serviceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: desc,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A service account: a machine principal the tenant owns, for CI and automation. " +
			"It is a client of the admin issuer, made with a member seat at the tenant and no secret: " +
			"mint a secret with `authwise_service_account_secret`. Its token, asked for at the estate audience, is accepted by kit's admin API.\n\n" +
			"`roles` are what it may do at its tenant, and change in place; every other argument replaces the account. " +
			"Destroying it unbinds its `roles` first. kit refuses to delete it while any other binding names it. " +
			"Service accounts are served at central only: against a satellite, kit refuses and names central.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: computed("Full resource name (`tenants/{t}/service-accounts/{c}`); serves as the Terraform ID, and is what `authwise_service_account_secret` takes."),
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The tenant that owns the account; overrides the provider default. Changing it replaces the account.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			"display_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique among the tenant's service accounts. Changing it replaces the account.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			expiresAtAttribute: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "When set (RFC 3339), the token endpoint refuses the account after it. Changing it replaces the account.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"roles": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The roles the account holds at its tenant, by key: `tenant.admin`, `tenant.viewer` or `guardcontrol.admin` (kit refuses any other). " +
					"Set, it is authoritative: the account holds exactly these, and an empty set unbinds them all. Unset, Terraform leaves the account's roles alone and reads them back.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"client_id": computed("The client's id (`c-…`): the `client_id` at the token endpoint, and the subject its tokens carry, which an `authwise_access_binding` names with `subject_type = \"client\"`."),
			"status":    computed("`CLIENT_STATUS_ACTIVE` unless an operator disabled or quarantined the client, as on `authwise_client`."),
			"secrets": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The live secrets, newest last: unexpired, and never the plaintext.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":               schema.StringAttribute{Computed: true, MarkdownDescription: "The secret's id: the part of the minted secret before the underscore."},
						createdAtAttribute: schema.StringAttribute{Computed: true},
						expiresAtAttribute: schema.StringAttribute{Computed: true, MarkdownDescription: "Unset: the secret does not expire."},
					},
				},
			},
			createdAtAttribute: computed(""),
			// updated_at and secrets move with a change of roles.
			"updated_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *serviceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = tenancyClientFrom(req.ProviderData, &resp.Diagnostics)
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.defaults = pd.Defaults
	}
}

func (r *serviceAccountResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("service_account resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *serviceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan serviceAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := r.createRequest(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.CreateServiceAccount(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("create service account failed", err.Error())
		return
	}

	roles := plan.Roles
	resp.Diagnostics.Append(plan.fromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Roles are a second call: create binds none. If it fails, the account
	// is kept in state, so Terraform taints it rather than losing it.
	if !roles.IsNull() && !roles.IsUnknown() {
		if out = r.setRoles(ctx, out.GetName(), roles, &resp.Diagnostics); out != nil {
			resp.Diagnostics.Append(plan.fromProto(ctx, out)...)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// createRequest builds the create from the plan: the tenant from the
// resource or the provider default, the display name and expiry.
func (r *serviceAccountResource) createRequest(plan *serviceAccountModel, diags *diag.Diagnostics) *tenancypb.CreateServiceAccountRequest {

	ids := map[string]string{tenantIDAttribute: r.defaults[tenantIDAttribute]}
	if v := plan.TenantID.ValueString(); v != "" {
		ids[tenantIDAttribute] = v
	}
	parent, err := serviceAccountScope.ComposeParent(ids)
	if err != nil {
		diags.AddError("cannot resolve parent for service account", err.Error()+"; set it on the resource or as a provider default")
		return nil
	}

	return &tenancypb.CreateServiceAccountRequest{
		Parent: parent,
		ServiceAccount: &tenancypb.ServiceAccount{
			DisplayName: plan.DisplayName.ValueString(),
			ExpiresAt:   parseTimestamp(plan.ExpiresAt, path.Root(expiresAtAttribute), diags),
		},
	}
}

// setRoles writes the account's roles; nil means it failed and diags says
// why.
func (r *serviceAccountResource) setRoles(ctx context.Context, name string, roles types.Set, diags *diag.Diagnostics) *tenancypb.ServiceAccount {

	var keys []string
	diags.Append(roles.ElementsAs(ctx, &keys, false)...)
	if diags.HasError() {
		return nil
	}

	out, err := r.client.SetServiceAccountRoles(ctx, &tenancypb.SetServiceAccountRolesRequest{Name: name, Roles: keys})
	switch {
	case err == nil:
		return out
	case status.Code(err) == codes.InvalidArgument:
		diags.AddAttributeError(path.Root("roles"), "kit refused the service account's roles",
			err.Error()+"\n\nA service account may hold only tenant.admin, tenant.viewer and guardcontrol.admin, and only those the estate's catalog declares.")
	default:
		diags.AddError("setting the service account's roles failed", err.Error())
	}
	return nil
}

func (r *serviceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state serviceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetServiceAccount(ctx, &tenancypb.GetServiceAccountRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read service account failed", err.Error())
		return
	}

	resp.Diagnostics.Append(state.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update writes roles, the one argument that changes in place.
func (r *serviceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state serviceAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Roles.IsUnknown() || plan.Roles.Equal(state.Roles) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	out := r.setRoles(ctx, state.Name.ValueString(), plan.Roles, &resp.Diagnostics)
	if out == nil {
		return
	}

	resp.Diagnostics.Append(state.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete surfaces kit's refusal while a role is bound to the account, with
// the fix; NotFound counts as already deleted.
func (r *serviceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state serviceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Its roles are its own, written only through SetServiceAccountRoles;
	// they go with it. Any other binding naming it still refuses.
	if len(state.Roles.Elements()) > 0 {
		_, err := r.client.SetServiceAccountRoles(ctx, &tenancypb.SetServiceAccountRolesRequest{Name: state.Name.ValueString()})
		if err != nil && !aip.IsNotFound(err) {
			resp.Diagnostics.AddError("unbinding the service account's roles failed", err.Error())
			return
		}
	}

	_, err := r.client.DeleteServiceAccount(ctx, &tenancypb.DeleteServiceAccountRequest{Name: state.Name.ValueString()})
	switch {
	case err == nil, aip.IsNotFound(err):
	case status.Code(err) == codes.FailedPrecondition:
		resp.Diagnostics.AddError("the service account is still bound",
			err.Error()+"\n\nkit refuses to delete a service account while a binding other than its roles names it. "+
				"Remove the binding first: referencing the account by expression (authwise_service_account.x.client_id) makes Terraform destroy the binding before the account.")
	default:
		resp.Diagnostics.AddError("delete service account failed", err.Error())
	}
}

// ImportState takes the full resource name.
func (r *serviceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := serviceAccountScope.ParseName(serviceAccountCollection, req.ID); err != nil {
		resp.Diagnostics.AddError("invalid import ID for service account", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), req.ID)...)
}

func (m *serviceAccountModel) fromProto(ctx context.Context, e *tenancypb.ServiceAccount) diag.Diagnostics {

	var diags diag.Diagnostics

	ids, _, err := serviceAccountScope.ParseName(serviceAccountCollection, e.GetName())
	if err != nil {
		diags.AddError("unexpected service account name", err.Error())
		return diags
	}

	m.Name = types.StringValue(e.GetName())
	m.TenantID = types.StringValue(ids[tenantIDAttribute])
	m.DisplayName = types.StringValue(e.GetDisplayName())
	m.ExpiresAt = timestampString(e.GetExpiresAt())
	m.ClientID = types.StringValue(e.GetClientId())
	roles, d := types.SetValueFrom(ctx, types.StringType, append([]string{}, e.GetRoles()...))
	diags.Append(d...)
	m.Roles = roles
	m.Status = types.StringValue(e.GetStatus().String())
	m.CreatedAt = timestampString(e.GetCreatedAt())
	m.UpdatedAt = timestampString(e.GetUpdatedAt())

	secretType := types.ObjectType{AttrTypes: serviceAccountSecretAttrTypes()}
	secrets := make([]attr.Value, 0, len(e.GetSecrets()))
	for _, s := range e.GetSecrets() {
		v, d := types.ObjectValue(serviceAccountSecretAttrTypes(), map[string]attr.Value{
			"id":               types.StringValue(s.GetId()),
			createdAtAttribute: timestampString(s.GetCreatedAt()),
			expiresAtAttribute: timestampString(s.GetExpiresAt()),
		})
		diags.Append(d...)
		secrets = append(secrets, v)
	}
	list, d := types.ListValue(secretType, secrets)
	diags.Append(d...)
	m.Secrets = list

	return diags
}

// --- authwise_service_account_secret ---

// serviceAccountSecretResource mints a secret for a service account. The
// secret is returned once, by the mint, so it lives in state. The tenancy
// service has no read or revoke of one secret: Read finds it among the
// account's live secrets, and Delete only forgets it.
type serviceAccountSecretResource struct {
	client tenancypb.AuthwiseTenancyServiceClient
}

type serviceAccountSecretModel struct {
	ServiceAccount types.String `tfsdk:"service_account"`
	ExpiresAt      types.String `tfsdk:"expires_at"`
	Rotate         types.Bool   `tfsdk:"rotate"`
	Grace          types.String `tfsdk:"grace"`
	Keepers        types.Map    `tfsdk:"keepers"`
	SecretID       types.String `tfsdk:"secret_id"`
	Secret         types.String `tfsdk:"secret"`
	TokenURL       types.String `tfsdk:"token_url"`
	Audience       types.String `tfsdk:"audience"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func newServiceAccountSecretResource() resource.Resource {
	return &serviceAccountSecretResource{}
}

func (r *serviceAccountSecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account_secret"
}

func (r *serviceAccountSecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: desc,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A service account's secret, for the client credentials grant. " +
			"kit mints it and returns it once, so the secret is kept in state: treat the state as sensitive.\n\n" +
			"**Rotation.** Replace the resource to rotate, for example by changing `keepers`. With `rotate` (the default), the mint gives the account's other live secrets `grace` to keep working, then they lapse, so a pipeline holding the old secret has that long to pick up the new one. " +
			"Two of these resources on one account with `rotate` left on would rotate each other out: set `rotate = false` on all but one.\n\n" +
			"**Destroying it does not revoke the secret.** The tenancy service has no revoke of one secret: the secret stops working when it expires, when a later mint rotates it, or when the account is deleted.",
		Attributes: map[string]schema.Attribute{
			"service_account": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The account's full resource name, `authwise_service_account.<name>.name`. Changing it mints a new secret.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			expiresAtAttribute: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "When the secret stops authenticating (RFC 3339, in the future). Unset: it does not expire. Changing it mints a new secret.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"rotate": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the mint gives the account's other live secrets `grace`, after which they lapse. Defaults to true. Only the mint reads it.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"grace": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "With `rotate`, how long the other secrets keep working, as a Go duration (`24h`, `72h`). Unset is 24 hours; kit allows at most 30 days. Only the mint reads it.",
			},
			"keepers": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Arbitrary values that mint a new secret when any of them changes: the rotation trigger.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"secret_id": computed("The secret's id, the part of `secret` before the underscore; it is listed in the account's `secrets`."),
			"secret": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "The client secret, from the mint. No read returns it again.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"token_url":        computed("Where to exchange it: the admin issuer's token endpoint."),
			"audience":         computed("The audience to ask for: the estate audience kit's admin API accepts."),
			createdAtAttribute: computed(""),
		},
	}
}

func (r *serviceAccountSecretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = tenancyClientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *serviceAccountSecretResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("service_account_secret resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *serviceAccountSecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan serviceAccountSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mint := &tenancypb.MintServiceAccountSecretRequest{
		Name:      plan.ServiceAccount.ValueString(),
		ExpiresAt: parseTimestamp(plan.ExpiresAt, path.Root(expiresAtAttribute), &resp.Diagnostics),
		Rotate:    plan.Rotate.ValueBool(),
	}
	if v := plan.Grace.ValueString(); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("grace"), "not a duration", err.Error())
		}
		mint.Grace = durationpb.New(d)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.MintServiceAccountSecret(ctx, mint)
	if err != nil {
		resp.Diagnostics.AddError("mint service account secret failed", err.Error())
		return
	}

	id, _, _ := strings.Cut(out.GetSecret(), "_")
	plan.SecretID = types.StringValue(id)
	plan.Secret = types.StringValue(out.GetSecret())
	plan.TokenURL = types.StringValue(out.GetTokenUrl())
	plan.Audience = types.StringValue(out.GetAudience())
	plan.CreatedAt = types.StringNull()
	if s := liveSecret(out.GetServiceAccount(), id); s != nil {
		plan.CreatedAt = timestampString(s.GetCreatedAt())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read keeps the secret while the account lists it as live. Once it has
// expired or lapsed after a rotation it is gone from state, and the next
// apply mints another.
func (r *serviceAccountSecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state serviceAccountSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetServiceAccount(ctx, &tenancypb.GetServiceAccountRequest{Name: state.ServiceAccount.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read service account failed", err.Error())
		return
	}

	if liveSecret(out, state.SecretID.ValueString()) == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update carries the mint-only arguments, rotate and grace, into state; no
// call is made.
func (r *serviceAccountSecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	var plan serviceAccountSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete forgets the secret: the tenancy service cannot revoke one.
func (r *serviceAccountSecretResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func liveSecret(sa *tenancypb.ServiceAccount, id string) *tenancypb.ServiceAccountSecret {
	for _, s := range sa.GetSecrets() {
		if s.GetId() == id {
			return s
		}
	}
	return nil
}
