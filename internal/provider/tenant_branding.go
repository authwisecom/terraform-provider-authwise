package provider

import (
	"context"
	"strings"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
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

const (
	tenantBrandingTypeName = "tenant_branding"

	// brandingSegment is the singleton's last segment: tenants/{t}/branding.
	brandingSegment = "branding"
)

var (
	_ resource.Resource                = &tenantBrandingResource{}
	_ resource.ResourceWithConfigure   = &tenantBrandingResource{}
	_ resource.ResourceWithImportState = &tenantBrandingResource{}
)

// tenantBrandingResource is authwise_tenant_branding (tf#75, kit#953): the
// tenant's brand, tenants/{t}/branding, a view of the stored
// TenantConfig.branding. It owns all four fields: every write is the whole
// brand, a field left out is cleared, and destroy clears the brand.
// authwise_tenant_config leaves branding alone when its config does not set
// it, so the two do not fight over it.
type tenantBrandingResource struct {
	client   identitypb.AuthwiseIdentityServiceClient
	defaults map[string]string
}

type tenantBrandingModel struct {
	Name            types.String `tfsdk:"name"`
	TenantID        types.String `tfsdk:"tenant_id"`
	DisplayName     types.String `tfsdk:"display_name"`
	ThemeID         types.String `tfsdk:"theme_id"`
	LogoAssetID     types.String `tfsdk:"logo_asset_id"`
	LogoDarkAssetID types.String `tfsdk:"logo_dark_asset_id"`
}

func newTenantBrandingResource() resource.Resource {
	return &tenantBrandingResource{}
}

func (r *tenantBrandingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + tenantBrandingTypeName
}

func (r *tenantBrandingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	resp.Schema = schema.Schema{
		MarkdownDescription: "A tenant's brand: its name, its look, and its logo. Mail and the sign-in page read it: " +
			"mail's From name and brand name, and the logo and colour from the theme.\n\n" +
			"The brand is authoritative: it becomes exactly this, a field left out is cleared, and destroying the resource clears the brand. " +
			"A tenant created on kit with kit#953 starts with a theme \"Brand\" named here: leaving `theme_id` out clears it, so name it (import it as `authwise_theme`) to keep it. " +
			"A new issuer of a tenant whose brand names a theme is given a \"Default\" appearance profile on that theme.\n\n" +
			"**Set a tenant's branding in one place.** This resource and `branding` in `authwise_tenant_config` write the same stored value. " +
			"`branding` there is deprecated: leave it out of that `config`, which then leaves the brand to this resource.\n\n" +
			"Name the theme and the logos through their resources (`authwise_theme.x.theme_id`, `authwise_asset_content.x.asset_id`). " +
			"kit refuses deleting a theme or emptying an asset the brand names, and the reference makes Terraform clear the brand before either goes.\n\n" +
			"Writing the brand needs `identity.themes.update` at the tenant.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The brand's full resource name (`tenants/{t}/branding`); serves as the Terraform ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The tenant; overrides the provider default. Changing it moves the brand: the old tenant's is cleared and the new one's written.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			displayName: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The brand's name, which mail shows and sends from (the From name). At most 64 characters on one line, none of `<`, `>` or `\"`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"theme_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The tenant's look: a theme of the tenant (`th-…`), from which mail takes its logo and colour.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"logo_asset_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "An image asset of the tenant (`as-…`): what the admin console shows in place of the Authwise emblem, and what mail falls back to when an appearance profile has no logo.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"logo_dark_asset_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The same, for a dark colour scheme. kit refuses it without `logo_asset_id`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *tenantBrandingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.defaults = pd.Defaults
	}
}

func (r *tenantBrandingResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("tenant_branding resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *tenantBrandingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan tenantBrandingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tenant := plan.TenantID.ValueString()
	if tenant == "" {
		tenant = r.defaults[tenantIDAttribute]
	}
	if tenant == "" {
		resp.Diagnostics.AddAttributeError(path.Root(tenantIDAttribute), "missing tenant_id",
			"set tenant_id on the resource or as a provider default")
		return
	}
	plan.Name = types.StringValue(tenantsCollection + "/" + tenant + "/" + brandingSegment)

	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tenantBrandingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state tenantBrandingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Name = state.Name

	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// write puts the whole brand: UpdateTenantBranding writes every field.
func (r *tenantBrandingResource) write(ctx context.Context, plan *tenantBrandingModel, diags *diag.Diagnostics) {

	defer lockTenantRow(plan.Name.ValueString())()
	_, err := r.client.UpdateTenantBranding(ctx, &identitypb.UpdateTenantBrandingRequest{
		Name: plan.Name.ValueString(),
		Branding: &corepb.Branding{
			DisplayName:     plan.DisplayName.ValueString(),
			ThemeId:         plan.ThemeID.ValueString(),
			LogoAssetId:     plan.LogoAssetID.ValueString(),
			LogoDarkAssetId: plan.LogoDarkAssetID.ValueString(),
		},
	})
	switch status.Code(err) {
	case codes.OK:
	case codes.InvalidArgument:
		diags.AddError("kit refused the brand", status.Convert(err).Message())
	case codes.PermissionDenied:
		diags.AddError("writing the brand was refused", status.Convert(err).Message()+
			"\n\nWriting a tenant's brand needs identity.themes.update at the tenant.")
	default:
		diags.AddError("writing the brand failed", err.Error())
	}
}

func (r *tenantBrandingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state tenantBrandingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetTenantBranding(ctx, &identitypb.GetTenantBrandingRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the brand failed", err.Error())
		return
	}

	state.DisplayName = stringOrNull(out.GetDisplayName())
	state.ThemeID = stringOrNull(out.GetThemeId())
	state.LogoAssetID = stringOrNull(out.GetLogoAssetId())
	state.LogoDarkAssetID = stringOrNull(out.GetLogoDarkAssetId())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete clears the brand. A tenant already gone has none.
func (r *tenantBrandingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state tenantBrandingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	defer lockTenantRow(state.Name.ValueString())()
	_, err := r.client.UpdateTenantBranding(ctx, &identitypb.UpdateTenantBrandingRequest{
		Name:     state.Name.ValueString(),
		Branding: &corepb.Branding{},
	})
	if err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("clearing the brand failed", err.Error())
	}
}

// ImportState takes the tenant's id (`t-…`), its name, or the brand's
// name. tenant_id is left null when it is the provider default.
func (r *tenantBrandingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	id := strings.TrimSuffix(strings.TrimPrefix(req.ID, tenantsCollection+"/"), "/"+brandingSegment)
	if !strings.HasPrefix(id, "t-") || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("invalid import ID for tenant branding",
			"expected the tenant's id (t-…), its name (tenants/t-…) or the brand's (tenants/t-…/branding), got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), tenantsCollection+"/"+id+"/"+brandingSegment)...)
	if id != r.defaults[tenantIDAttribute] {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), id)...)
	}
}

// stringOrNull is s, or null when empty: kit returns a cleared field as
// "", and the configuration leaves it out.
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
