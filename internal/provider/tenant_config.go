package provider

import (
	"context"
	"strings"

	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// authwise_tenant_config (#40) manages an existing tenant's TenantConfig,
// and optionally its labels, through the public tenancy service's
// PatchTenant. It never creates or deletes a tenant: tenants are awtenant's
// (#36). display_name is left out: awtenant and the console own it, and a
// destroy would have nothing to reset it to.

const (
	tenantsCollection = "tenants"

	configAttribute = "config"
	labelsAttribute = "labels"
)

var (
	_ resource.Resource                = &tenantConfigResource{}
	_ resource.ResourceWithConfigure   = &tenantConfigResource{}
	_ resource.ResourceWithImportState = &tenantConfigResource{}
)

type tenantConfigResource struct {
	client   tenancypb.AuthwiseTenancyServiceClient
	defaults map[string]string
}

type tenantConfigModel struct {
	Name     types.String         `tfsdk:"name"`
	TenantID types.String         `tfsdk:"tenant_id"`
	Config   jsontypes.Normalized `tfsdk:"config"`
	Labels   types.Map            `tfsdk:"labels"`
}

func newTenantConfigResource() resource.Resource {
	return &tenantConfigResource{}
}

func (r *tenantConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_config"
}

func (r *tenantConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	resp.Schema = schema.Schema{
		MarkdownDescription: "The config of an existing tenant: its brand, and its token lifetimes and CORS. " +
			"The tenant itself is not created or deleted here: tenants are provisioned with awtenant.\n\n" +
			"`config` is authoritative: the tenant's config becomes exactly this, and a field left out is cleared. " +
			"Creating the resource replaces whatever config the tenant had. " +
			"Destroying it clears the config (and `labels`, when set) rather than only dropping it from state, " +
			"so the assets the brand names can be deleted after it. " +
			"Reference the logo as `authwise_asset_content.x.asset_id`, not `authwise_asset.x.asset_id`: " +
			"kit refuses deleting an asset, or removing its content, while the brand names it, " +
			"and the reference makes Terraform destroy this resource before both.\n\n" +
			"Writing a tenant needs `tenancy.tenants.update` at the tenant, which a service account holds through `tenant.admin`.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The tenant's full resource name (`tenants/{t}`); serves as the Terraform ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The tenant; overrides the provider default. Changing it moves the config: the old tenant's is cleared and the new one's written.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			configAttribute: schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "The TenantConfig as protojson: `data.authwise_tenant_config.x.json`, or `jsonencode`. " +
					"kit refuses a `branding` that names no image asset of the tenant, or a dark logo without a light one.",
			},
			labelsAttribute: schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The tenant's labels. Set, they are authoritative, and destroy clears them. " +
					"Unset, Terraform leaves the tenant's labels alone.",
			},
		},
	}
}

func (r *tenantConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = tenancyClientFrom(req.ProviderData, &resp.Diagnostics)
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.defaults = pd.Defaults
	}
}

func (r *tenantConfigResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("tenant_config resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *tenantConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan tenantConfigModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tenantID := plan.TenantID.ValueString()
	if tenantID == "" {
		tenantID = r.defaults[tenantIDAttribute]
	}
	if tenantID == "" {
		resp.Diagnostics.AddAttributeError(path.Root(tenantIDAttribute), "missing tenant_id",
			"set tenant_id on the resource or as a provider default")
		return
	}
	plan.Name = types.StringValue(tenantsCollection + "/" + tenantID)
	plan.TenantID = types.StringValue(tenantID)

	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tenantConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state tenantConfigModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Name, plan.TenantID = state.Name, state.TenantID

	// labels no longer managed: left as they are, as unset labels are
	// on create.
	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// write patches the plan's config, and its labels when set, onto the
// tenant. The mask names only those, so the fields this resource does not
// manage are kept.
func (r *tenantConfigResource) write(ctx context.Context, plan *tenantConfigModel, diags *diag.Diagnostics) {

	config := &corepb.TenantConfig{}
	if err := protojson.Unmarshal([]byte(plan.Config.ValueString()), config); err != nil {
		diags.AddAttributeError(path.Root(configAttribute), "config is not a TenantConfig", err.Error())
		return
	}

	tenant := &corepb.Tenant{Config: config}
	paths := []string{configAttribute}
	if !plan.Labels.IsNull() {
		labels := map[string]string{}
		diags.Append(plan.Labels.ElementsAs(ctx, &labels, false)...)
		if diags.HasError() {
			return
		}
		tenant.Labels = labels
		paths = append(paths, labelsAttribute)
	}

	_, err := r.client.PatchTenant(ctx, &tenancypb.PatchTenantRequest{
		Name:       plan.Name.ValueString(),
		Tenant:     tenant,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
	})
	switch {
	case err == nil:
	case status.Code(err) == codes.InvalidArgument:
		diags.AddAttributeError(path.Root(configAttribute), "kit refused the tenant config", err.Error())
	case status.Code(err) == codes.PermissionDenied:
		diags.AddError("writing the tenant config was refused", err.Error()+
			"\n\nWriting a tenant needs tenancy.tenants.update at the tenant. A service account holds it through tenant.admin, "+
			"from kit v1.45.0 (on an estate that was already running, once the role is updated at central); earlier kit grants it only to the tenant's owners and the platform's tenancy roles.")
	default:
		diags.AddError("writing the tenant config failed", err.Error())
	}
}

func (r *tenantConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state tenantConfigModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetTenant(ctx, &tenancypb.GetTenantRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read tenant failed", err.Error())
		return
	}

	resp.Diagnostics.Append(state.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// fromProto reads the tenant into the model. The config is kept as written
// while it means what the tenant holds, so a hand-written jsonencode, with
// snake_case keys or zero values kit leaves out, is no drift; otherwise it
// is the tenant's, re-encoded as the data source encodes it. Labels are read
// when managed, and on import.
func (m *tenantConfigModel) fromProto(ctx context.Context, t *corepb.Tenant) diag.Diagnostics {

	var diags diag.Diagnostics

	imported := m.Config.IsNull()

	held := t.GetConfig()
	if held == nil {
		held = &corepb.TenantConfig{}
	}
	written := &corepb.TenantConfig{}
	if imported || protojson.Unmarshal([]byte(m.Config.ValueString()), written) != nil || !proto.Equal(written, held) {
		doc, err := configJSON(held)
		if err != nil {
			diags.AddError("cannot encode the tenant config", err.Error())
			return diags
		}
		m.Config = jsontypes.NewNormalizedValue(doc)
	}

	m.Name = types.StringValue(t.GetName())
	m.TenantID = types.StringValue(strings.TrimPrefix(t.GetName(), tenantsCollection+"/"))

	if !m.Labels.IsNull() || (imported && len(t.GetLabels()) > 0) {
		labels, d := types.MapValueFrom(ctx, types.StringType, t.GetLabels())
		diags.Append(d...)
		m.Labels = labels
	}

	return diags
}

// Delete clears the config, and the labels when managed, under the same
// mask. kit refuses deleting an asset the brand names, so this is what lets
// the assets go after it.
func (r *tenantConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state tenantConfigModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	paths := []string{configAttribute}
	if !state.Labels.IsNull() {
		paths = append(paths, labelsAttribute)
	}

	_, err := r.client.PatchTenant(ctx, &tenancypb.PatchTenantRequest{
		Name:       state.Name.ValueString(),
		Tenant:     &corepb.Tenant{Config: &corepb.TenantConfig{}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
	})
	if err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("clearing the tenant config failed", err.Error())
	}
}

// ImportState takes the tenant's id (`t-…`) or its full name.
func (r *tenantConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	id := strings.TrimPrefix(req.ID, tenantsCollection+"/")
	if !strings.HasPrefix(id, "t-") || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("invalid import ID for tenant config",
			"expected the tenant's id (t-…) or its full name (tenants/t-…), got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), tenantsCollection+"/"+id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), id)...)
}
