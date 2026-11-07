package provider

import (
	"context"
	"strings"

	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// authwise_tenant_home_administration (tf#70, kit#916, HOME_ADMINISTRATION
// HA5): the tenant's home administration console turned on at one of its
// issuers. The entry is Tenant.products["administration"], the TenantProduct
// row a product's entry is; EnableHomeAdministration on the public tenancy
// service creates it, idempotently. There is no disable on the public
// service: an operator's DisableProduct("administration") on the tenancy
// admin listener is the only way off, so destroy forgets.

const (
	// administrationProduct is the entry's key in Tenant.products.
	administrationProduct   = "administration"
	administrationAttribute = "administration"
	audienceIDAttribute     = "audience_id"
	consoleClientIDAttr     = "console_client_id"
)

var administrationAttrTypes = map[string]attr.Type{
	issuerIDAttribute:   types.StringType,
	audienceIDAttribute: types.StringType,
	consoleClientIDAttr: types.StringType,
}

// administrationSchema is the read-only entry on authwise_tenant_config.
func administrationSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Computed: true,
		MarkdownDescription: "The tenant's home administration entry (kit#916), read only: null until it is enabled with `authwise_tenant_home_administration`. " +
			"Its host serves the administration console at `/admin/`.",
		Attributes: map[string]schema.Attribute{
			issuerIDAttribute:   schema.StringAttribute{Computed: true, MarkdownDescription: "The issuer (`i-…`) the tenant's administrators sign in at."},
			audienceIDAttribute: schema.StringAttribute{Computed: true, MarkdownDescription: "The administration audience (`a-…`) kit created on it."},
			consoleClientIDAttr: schema.StringAttribute{Computed: true, MarkdownDescription: "The console's public client (`c-…`) on the issuer."},
		},
	}
}

// administrationValue is the tenant's entry as an object, null without one.
func administrationValue(t *corepb.Tenant) (types.Object, diag.Diagnostics) {
	e, ok := t.GetProducts()[administrationProduct]
	if !ok || e == nil {
		return types.ObjectNull(administrationAttrTypes), nil
	}
	return types.ObjectValue(administrationAttrTypes, map[string]attr.Value{
		issuerIDAttribute:   types.StringValue(e.GetIssuerId()),
		audienceIDAttribute: types.StringValue(e.GetAudienceId()),
		consoleClientIDAttr: types.StringValue(e.GetConsoleClientId()),
	})
}

var (
	_ resource.Resource                = &tenantHomeAdministrationResource{}
	_ resource.ResourceWithConfigure   = &tenantHomeAdministrationResource{}
	_ resource.ResourceWithImportState = &tenantHomeAdministrationResource{}
)

type tenantHomeAdministrationResource struct {
	client   tenancypb.AuthwiseTenancyServiceClient
	defaults map[string]string
}

type tenantHomeAdministrationModel struct {
	Name            types.String `tfsdk:"name"`
	TenantID        types.String `tfsdk:"tenant_id"`
	IssuerID        types.String `tfsdk:"issuer_id"`
	AudienceID      types.String `tfsdk:"audience_id"`
	ConsoleClientID types.String `tfsdk:"console_client_id"`
}

func newTenantHomeAdministrationResource() resource.Resource {
	return &tenantHomeAdministrationResource{}
}

func (r *tenantHomeAdministrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_home_administration"
}

func (r *tenantHomeAdministrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Turns on the tenant's home administration (kit#916): its administrators sign in at one of its issuers, and that issuer's host serves the administration console at `/admin/`. " +
			"kit creates the administration audience, its catalog and the console's client, and records them on the tenant (`authwise_tenant_config.administration` reads them too).\n\n" +
			"Enabling is idempotent. **There is no disable here:** destroying the resource only removes it from state, and the entry stays enabled. " +
			"An operator's `DisableProduct(\"administration\")` on the tenancy admin service is the only way off.\n\n" +
			"Enabling needs `tenancy.administration.enable` at the tenant, which only `tenancy.owner` holds: a service account with `tenant.admin` is refused. " +
			"Run it with the tenant owner's credentials.\n\n" +
			"The issuer needs a binding at the root of a host (`authwise_issuer_domain_binding` with no `path`): home administration exists only on a host that names one tenant at its root.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The tenant's name, `tenants/{t}`; serves as the Terraform ID.",
				PlanModifiers:       keep,
			},
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The tenant; overrides the provider default. Changing it enables the new tenant and leaves the old one enabled.",
				PlanModifiers:       []planmodifier.String{tfruntime.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			issuerIDAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The tenant's issuer (`i-…`) its administrators sign in at, `authwise_issuer.<name>.issuer_id`. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{tfruntime.ReferenceID("i", "issuer", "authwise_issuer.<name>.issuer_id")},
			},
			audienceIDAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The administration audience (`a-…`) kit created on the issuer.",
				PlanModifiers:       keep,
			},
			consoleClientIDAttr: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The console's public client (`c-…`) on the issuer.",
				PlanModifiers:       keep,
			},
		},
	}
}

func (r *tenantHomeAdministrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, pd := providerClient[tenancypb.AuthwiseTenancyServiceClient](req, tenancyClientKey, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client, r.defaults = client, pd.Defaults
}

func (r *tenantHomeAdministrationResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("tenant_home_administration resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *tenantHomeAdministrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan tenantHomeAdministrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tenantID := plan.TenantID.ValueString()
	if tenantID == "" {
		tenantID = r.defaults[tenantIDAttribute]
	}
	if tenantID == "" {
		resp.Diagnostics.AddAttributeError(path.Root(tenantIDAttribute), "missing tenant_id", "set tenant_id on the resource or as a provider default")
		return
	}

	t, err := r.client.EnableHomeAdministration(ctx, &tenancypb.EnableHomeAdministrationRequest{
		Name:     tenantsCollection + "/" + tenantID,
		IssuerId: plan.IssuerID.ValueString(),
	})
	if err != nil {
		summary, detail := "enabling home administration failed", err.Error()
		if status.Code(err) == codes.PermissionDenied {
			summary = "enabling home administration was refused"
			detail += "\n\nIt needs tenancy.administration.enable at the tenant, which only tenancy.owner holds; a service account with tenant.admin is refused. " +
				"Run it with the tenant owner's credentials, or have an operator enable it with EnableProduct(\"administration\")."
		}
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	if !plan.fromTenant(t, &resp.Diagnostics) {
		resp.Diagnostics.AddError("home administration not recorded", "kit returned the tenant without an administration entry")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// fromTenant reads the entry; false when the tenant has none.
func (m *tenantHomeAdministrationModel) fromTenant(t *corepb.Tenant, _ *diag.Diagnostics) bool {
	e, ok := t.GetProducts()[administrationProduct]
	if !ok || e == nil {
		return false
	}
	m.Name = types.StringValue(t.GetName())
	m.TenantID = types.StringValue(strings.TrimPrefix(t.GetName(), tenantsCollection+"/"))
	m.IssuerID = types.StringValue(e.GetIssuerId())
	m.AudienceID = types.StringValue(e.GetAudienceId())
	m.ConsoleClientID = types.StringValue(e.GetConsoleClientId())
	return true
}

// Read follows the entry: gone (an operator disabled it) removes the
// resource, so the next plan enables it again.
func (r *tenantHomeAdministrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state tenantHomeAdministrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	t, err := r.client.GetTenant(ctx, &tenancypb.GetTenantRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read tenant failed", err.Error())
		return
	}
	if !state.fromTenant(t, &resp.Diagnostics) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update has nothing to send: every input replaces.
func (r *tenantHomeAdministrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan tenantHomeAdministrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete forgets: the public tenancy service has no disable.
func (r *tenantHomeAdministrationResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("home administration is still enabled",
		"Destroying authwise_tenant_home_administration removes it from Terraform state only: the tenant's administration entry, audience and console client stay. "+
			"An operator's DisableProduct(\"administration\") on the tenancy admin service is the only way to turn it off.")
}

// ImportState takes the tenant, tenants/{t} or its id.
func (r *tenantHomeAdministrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimPrefix(req.ID, tenantsCollection+"/")
	if id == "" || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("invalid import ID for tenant_home_administration", "expected tenants/{t} or the tenant id; got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), tenantsCollection+"/"+id)...)
}
