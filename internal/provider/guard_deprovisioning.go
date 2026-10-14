package provider

import (
	"context"
	"strings"
	"time"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	guardDeprovisioningTypeName = "guard_deprovisioning"
	audienceAttribute           = "audience"
)

// errDeprovisioningUnavailable is why no account can be set: guard-control
// says so in GetDeprovisioning's available.
const errDeprovisioningUnavailable = "this guard-control install cannot sweep: it has no sweep.sealKeyFile, or runs in local access mode. " +
	"No deprovisioning account can be set until it does."

var (
	_ resource.Resource                = &guardDeprovisioningResource{}
	_ resource.ResourceWithConfigure   = &guardDeprovisioningResource{}
	_ resource.ResourceWithImportState = &guardDeprovisioningResource{}
)

// guardDeprovisioningResource is authwise_guard_deprovisioning: the service
// account guard-control's deprovisioning sweep reads the users-link home
// with (guard-control#56, TENANT_PEOPLE.md §3.5). One per Guard tenant, so
// the tenant is its identity.
//
// The secret is a write-only argument, as authwise_secret's material is:
// guard-control seals it and never returns it. SetDeprovisioningAccount
// takes the whole account every time, so each create and update sends the
// configured secret; client_secret_wo_version exists to force an update
// when only the secret changed. The sweep's last result is on the data
// source, so a refresh does not churn the resource.
type guardDeprovisioningResource struct {
	client guardpb.GuardTenantAdminServiceClient
}

type guardDeprovisioningModel struct {
	TenantID              types.String `tfsdk:"tenant_id"`
	ClientID              types.String `tfsdk:"client_id"`
	ClientSecretWO        types.String `tfsdk:"client_secret_wo"`
	ClientSecretWOVersion types.Int64  `tfsdk:"client_secret_wo_version"`
	TokenURL              types.String `tfsdk:"token_url"`
	Audience              types.String `tfsdk:"audience"`
}

func newGuardDeprovisioningResource() resource.Resource {
	return &guardDeprovisioningResource{}
}

func (r *guardDeprovisioningResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardDeprovisioningTypeName
}

func (r *guardDeprovisioningResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The service account a Guard tenant's deprovisioning sweep uses. A device a person enrolled keeps working after they lose Guard at the users-link home, " +
			"because its token refreshes on the node's own identity. The sweep re-checks with this account and revokes each node whose owner no enrolment grant reaches any more. " +
			"It never revokes on a failed read: that is a warning, readable on the `authwise_guard_deprovisioning` data source.\n\n" +
			"The account is a kit service account of the users-link tenant holding `tenant.viewer` there, for example an `authwise_service_account` and its `authwise_service_account_secret`. " +
			"guard-control mints a token with it and reads the home once before storing it, and refuses a credential that cannot do both; nothing is stored then.\n\n" +
			"**The secret.** `client_secret_wo` is write-only (Terraform 1.11 or later): guard-control seals it, and it is not in the plan, not in state, and no API call returns it. " +
			"Every apply that changes the account sends it again. To send a new secret alone, bump `client_secret_wo_version`.\n\n" +
			"Destroying the resource clears the account, and the sweep stops for the tenant. The guard-control install needs `sweep.sealKeyFile` and remote access mode.",
		Attributes: map[string]schema.Attribute{
			tenantIDAttribute: schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"client_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The service account's client id.",
			},
			"client_secret_wo": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				WriteOnly:           true,
				MarkdownDescription: "The service account's client secret, write-only. Sent on create and on every update; never stored.",
			},
			"client_secret_wo_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Change this to send `client_secret_wo` again when nothing else about the account changed.",
			},
			"token_url": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The token endpoint the account's client credentials are exchanged at.",
			},
			audienceAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The audience the sweep asks its token for: the users-link home's admin API.",
			},
		},
	}
}

func (r *guardDeprovisioningResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = guardTenantAdminClientFrom(req.ProviderData)
}

func (r *guardDeprovisioningResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_guard_deprovisioning is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func (r *guardDeprovisioningResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, config guardDeprovisioningModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Say why guard-control would refuse, rather than relay its refusal.
	got, err := r.client.GetDeprovisioning(ctx, &guardpb.GetDeprovisioningRequest{Name: guardTenantName(plan.TenantID.ValueString())})
	if err != nil {
		resp.Diagnostics.AddError("reading deprovisioning failed", err.Error())
		return
	}
	if !got.GetAvailable() {
		resp.Diagnostics.AddError("deprovisioning is not available", errDeprovisioningUnavailable)
		return
	}

	if r.set(ctx, &plan, config.ClientSecretWO, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// set stores the planned account with the configured secret, and reports
// whether it was stored.
func (r *guardDeprovisioningResource) set(ctx context.Context, plan *guardDeprovisioningModel, secret types.String, diags *diag.Diagnostics) bool {

	if secret.IsNull() || secret.IsUnknown() {
		diags.AddAttributeError(path.Root("client_secret_wo"), "client_secret_wo is required",
			"guard-control takes the whole account on every change; set client_secret_wo (Terraform 1.11 or later).")
		return false
	}

	_, err := r.client.SetDeprovisioningAccount(ctx, &guardpb.SetDeprovisioningAccountRequest{
		Name:         guardTenantName(plan.TenantID.ValueString()),
		ClientId:     plan.ClientID.ValueString(),
		ClientSecret: secret.ValueString(),
		TokenUrl:     plan.TokenURL.ValueString(),
		Audience:     plan.Audience.ValueString(),
	})
	if err != nil {
		diags.AddError("setting the deprovisioning account failed", err.Error()+
			"\n\nguard-control mints a token with the account and reads the users-link home once before storing it; a credential that cannot do both is refused.")
		return false
	}

	plan.ClientSecretWO = types.StringNull()
	return true
}

// Read removes the resource when no account is stored any more, and
// otherwise reads back what guard-control holds, so an account changed
// elsewhere shows as drift. The secret cannot be compared.
func (r *guardDeprovisioningResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardDeprovisioningModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetDeprovisioning(ctx, &guardpb.GetDeprovisioningRequest{Name: guardTenantName(state.TenantID.ValueString())})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading deprovisioning failed", err.Error())
		return
	}
	if !out.GetConfigured() {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ClientID = types.StringValue(out.GetClientId())
	state.TokenURL = types.StringValue(out.GetTokenUrl())
	state.Audience = types.StringValue(out.GetAudience())
	state.ClientSecretWO = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *guardDeprovisioningResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, config guardDeprovisioningModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if r.set(ctx, &plan, config.ClientSecretWO, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete clears the account; NotFound counts as already cleared.
func (r *guardDeprovisioningResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardDeprovisioningModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ClearDeprovisioningAccount(ctx, &guardpb.ClearDeprovisioningAccountRequest{Name: guardTenantName(state.TenantID.ValueString())})
	if err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("clearing the deprovisioning account failed", err.Error())
	}
}

// ImportState takes the Guard tenant's id (gt-…) or name (tenants/gt-…).
// The secret cannot be imported: the next apply that changes the account,
// or bumps client_secret_wo_version, sends the configured one.
func (r *guardDeprovisioningResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	id := strings.TrimPrefix(req.ID, tenantsCollection+"/")
	if !strings.HasPrefix(id, "gt-") || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("invalid import ID for deprovisioning",
			"expected the Guard tenant's id (gt-…) or its full name (tenants/gt-…), got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), id)...)
}

// --- data source ---

var (
	_ datasource.DataSource              = &guardDeprovisioningDataSource{}
	_ datasource.DataSourceWithConfigure = &guardDeprovisioningDataSource{}
)

// guardDeprovisioningDataSource reads a Guard tenant's sweep: its account
// (never the secret) and the last sweep's result.
type guardDeprovisioningDataSource struct {
	client guardpb.GuardTenantAdminServiceClient
}

type guardDeprovisioningDataSourceModel struct {
	TenantID      types.String `tfsdk:"tenant_id"`
	Available     types.Bool   `tfsdk:"available"`
	Configured    types.Bool   `tfsdk:"configured"`
	ClientID      types.String `tfsdk:"client_id"`
	TokenURL      types.String `tfsdk:"token_url"`
	Audience      types.String `tfsdk:"audience"`
	LastSweepTime types.String `tfsdk:"last_sweep_time"`
	LastResult    types.String `tfsdk:"last_result"`
	LastMessage   types.String `tfsdk:"last_message"`
	LastRevoked   types.List   `tfsdk:"last_revoked"`
}

func newGuardDeprovisioningDataSource() datasource.DataSource {
	return &guardDeprovisioningDataSource{}
}

func (d *guardDeprovisioningDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardDeprovisioningTypeName
}

func (d *guardDeprovisioningDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		MarkdownDescription: "Reads a Guard tenant's deprovisioning sweep: the account it uses (never the secret) and what the last sweep did.",
		Attributes: map[string]dsschema.Attribute{
			tenantIDAttribute: dsschema.StringAttribute{Required: true},
			"available": dsschema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "False when the guard-control install has no `sweep.sealKeyFile` or runs in local access mode: no account can be set and nothing is swept.",
			},
			"configured":      dsschema.BoolAttribute{Computed: true, MarkdownDescription: "True when an account is stored."},
			"client_id":       dsschema.StringAttribute{Computed: true},
			"token_url":       dsschema.StringAttribute{Computed: true},
			"audience":        dsschema.StringAttribute{Computed: true},
			"last_sweep_time": dsschema.StringAttribute{Computed: true, MarkdownDescription: "When the tenant was last swept, RFC 3339; null before the first sweep."},
			"last_result": dsschema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`ok`, or `warning` when the sweep could not read what it needs or refused to revoke. A warning revokes nothing.",
			},
			"last_message": dsschema.StringAttribute{Computed: true, MarkdownDescription: "What the last sweep found, or why it warned."},
			"last_revoked": dsschema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The nodes the last sweep revoked, by resource name.",
			},
		},
	}
}

func (d *guardDeprovisioningDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = guardTenantAdminClientFrom(req.ProviderData)
}

func (d *guardDeprovisioningDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if d.client == nil {
		resp.Diagnostics.AddError("authwise_guard_deprovisioning data source not configured", "Configure was not called with the provider's data")
		return
	}

	var m guardDeprovisioningDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := d.client.GetDeprovisioning(ctx, &guardpb.GetDeprovisioningRequest{Name: guardTenantName(m.TenantID.ValueString())})
	if err != nil {
		resp.Diagnostics.AddError("reading deprovisioning failed", err.Error())
		return
	}

	m.Available = types.BoolValue(out.GetAvailable())
	m.Configured = types.BoolValue(out.GetConfigured())
	m.ClientID = types.StringValue(out.GetClientId())
	m.TokenURL = types.StringValue(out.GetTokenUrl())
	m.Audience = types.StringValue(out.GetAudience())
	m.LastResult = types.StringValue(out.GetLastResult())
	m.LastMessage = types.StringValue(out.GetLastMessage())

	m.LastSweepTime = types.StringNull()
	if ts := out.GetLastSweepTime(); ts != nil {
		m.LastSweepTime = types.StringValue(ts.AsTime().UTC().Format(time.RFC3339))
	}

	revoked, diags := types.ListValueFrom(ctx, types.StringType, out.GetLastRevoked())
	resp.Diagnostics.Append(diags...)
	m.LastRevoked = revoked

	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
