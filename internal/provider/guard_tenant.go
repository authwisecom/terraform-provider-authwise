package provider

import (
	"context"
	"regexp"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// guardDirectoryClientKey is the ProviderData.Clients key of guard-control's
// GuardDirectoryService client.
const guardDirectoryClientKey = "guard_directory"

// guardSlugPattern is a Guard tenant's slug: a DNS label (kit
// docswip/GUARD_HOME_USERS_CONSOLE.md §3.4).
var guardSlugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

var _ datasource.DataSourceWithConfigure = &guardTenantDataSource{}

// guardTenantDataSource is authwise_guard_tenant: the generated read by name,
// or by slug (#42). A slug is resolved to the tenant's id through
// guard-control's ResolveTenant, which is unauthenticated, and the tenant is
// then read by name as before; so a configuration names the tenant by slug
// and every resource still references it by its id.
type guardTenantDataSource struct {
	inner     datasource.DataSource
	directory guardpb.GuardDirectoryServiceClient
}

func newGuardTenantDataSource() datasource.DataSource {
	return &guardTenantDataSource{inner: generated.NewTenantDataSource()}
}

func (d *guardTenantDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	d.inner.Metadata(ctx, req, resp)
}

func (d *guardTenantDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {

	d.inner.Schema(ctx, req, resp)

	resp.Schema.MarkdownDescription = "A Guard tenant, under a parent Authwise tenant. Nothing else in Guard exists until this does. awtenant creates it; the provider only reads it. " +
		"`users` is where the tenant's people sign in and are decided. This data source reads one by its full resource name, or by its slug.\n\n" +
		"A slug is an alias of the tenant's id, never a reference: look the tenant up by slug and pass `guard_tenant_id` to other resources. " +
		"A renamed tenant's old slug resolves to nothing."

	name := resp.Schema.Attributes[tfruntime.NameAttribute].(dsschema.StringAttribute)
	name.Required, name.Optional = false, true
	name.Computed = true
	name.MarkdownDescription = "Full resource name of the Guard tenant to read (`tenants/gt-…`). Exactly one of `name` and `slug`."
	resp.Schema.Attributes[tfruntime.NameAttribute] = name

	resp.Schema.Attributes["slug"] = dsschema.StringAttribute{
		Optional: true,
		Computed: true,
		MarkdownDescription: "The Guard tenant's slug, a DNS label (`millcreek`), resolved to the tenant through guard-control's `ResolveTenant`. " +
			"Exactly one of `name` and `slug`. Empty on a tenant read by name that has none. A lookup by slug needs guard-control with the directory (guard-control#57).",
		Validators: []validator.String{
			stringvalidator.ExactlyOneOf(path.MatchRoot(tfruntime.NameAttribute)),
			stringvalidator.RegexMatches(guardSlugPattern, "must be a DNS label: lower-case letters, digits and hyphens, at most 63, not starting or ending with a hyphen"),
		},
	}
}

func (d *guardTenantDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if inner, ok := d.inner.(datasource.DataSourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		d.directory, _ = pd.Clients[guardDirectoryClientKey].(guardpb.GuardDirectoryServiceClient)
	}
}

// Read resolves a slug to the tenant's name, then reads the tenant by name.
func (d *guardTenantDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	var slug types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("slug"), &slug)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !slug.IsNull() {
		if d.directory == nil {
			resp.Diagnostics.AddError("authwise_guard_tenant is not configured", "Configure was not called with the provider's data")
			return
		}

		out, err := d.directory.ResolveTenant(ctx, &guardpb.ResolveTenantRequest{Key: &guardpb.ResolveTenantRequest_Slug{Slug: slug.ValueString()}})
		if err != nil {
			detail := err.Error()
			if aip.IsNotFound(err) {
				detail = "no Guard tenant has the slug " + slug.ValueString() + ": it is unknown, reserved, or was renamed (" + detail + ")"
			}
			resp.Diagnostics.AddAttributeError(path.Root("slug"), "resolving the Guard tenant failed", detail)
			return
		}

		// Read by name, as a configuration naming it would.
		byName := tfsdk.State{Schema: req.Config.Schema, Raw: req.Config.Raw.Copy()}
		resp.Diagnostics.Append(byName.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), guardTenantName(out.GetId()))...)
		if resp.Diagnostics.HasError() {
			return
		}
		req.Config = tfsdk.Config{Schema: req.Config.Schema, Raw: byName.Raw}
	}

	d.inner.Read(ctx, req, resp)
}
