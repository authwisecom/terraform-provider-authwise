package provider

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// installStatusParent is the whole install: GetInstallStatus has no other
// parent.
const installStatusParent = "tenants/-"

// The install status's attribute names that other files spell too.
const (
	installStateAttribute     = "state"
	installPlanAttribute      = "plan"
	installUpdatedAtAttribute = "updated_at"
)

var (
	_ datasource.DataSource              = &installStatusDataSource{}
	_ datasource.DataSourceWithConfigure = &installStatusDataSource{}
)

// installStatusDataSource is authwise_install_status (tf#77): kit's
// GetInstallStatus (apis v0.81.0, kit 1.94.0), a read of the install's
// license state and its registration with the portal that issued the
// license. Only the install's operators may read it. The license itself is
// kit configuration the operator sets, so there is no resource.
type installStatusDataSource struct {
	client identitypb.AuthwiseIdentityServiceClient
}

type installStatusModel struct {
	Name         types.String `tfsdk:"name"`
	ID           types.String `tfsdk:"id"`
	URL          types.String `tfsdk:"url"`
	KitVersion   types.String `tfsdk:"kit_version"`
	License      types.Object `tfsdk:"license"`
	Registration types.Object `tfsdk:"registration"`
}

type installLicenseModel struct {
	State      types.String `tfsdk:"state"`
	LicenseID  types.String `tfsdk:"license_id"`
	Org        types.String `tfsdk:"org"`
	Plan       types.String `tfsdk:"plan"`
	Exp        types.String `tfsdk:"exp"`
	GraceUntil types.String `tfsdk:"grace_until"`
	CheckedAt  types.String `tfsdk:"checked_at"`
	Keys       types.Object `tfsdk:"keys"`
}

type installLicenseKeysModel struct {
	Source    types.String `tfsdk:"source"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

type installRegistrationModel struct {
	Enabled       types.Bool   `tfsdk:"enabled"`
	LastAttemptAt types.String `tfsdk:"last_attempt_at"`
	LastResult    types.String `tfsdk:"last_result"`
	Payload       types.String `tfsdk:"payload"`
}

func installLicenseKeysAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"source": types.StringType, installUpdatedAtAttribute: types.StringType}
}

func installLicenseAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		installStateAttribute: types.StringType, "license_id": types.StringType, "org": types.StringType, installPlanAttribute: types.StringType,
		"exp": types.StringType, "grace_until": types.StringType, "checked_at": types.StringType,
		"keys": types.ObjectType{AttrTypes: installLicenseKeysAttrTypes()},
	}
}

func installRegistrationAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		settingsEnabledAttribute: types.BoolType, "last_attempt_at": types.StringType, "last_result": types.StringType, "payload": types.StringType,
	}
}

func newInstallStatusDataSource() datasource.DataSource {
	return &installStatusDataSource{}
}

func (d *installStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_install_status"
}

func (d *installStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "The install's license state and its registration with the portal that issued the license, as of kit's last check (at boot and daily). " +
			"Only the install's operators may read it: any other credential is refused. " +
			"The license itself is kit configuration (`license.token`, `license.keys`), set by the operator, not a resource. " +
			"No state changes what the install does: it is recorded and reported only. Needs kit 1.94.0 or later.",
		Attributes: map[string]schema.Attribute{
			attrName:      computed("The install's name (`install.name`)."),
			"id":          computed("The install's id (`install.id`), set by the operator; empty when unset."),
			"url":         computed("The install's URL (`install.url`); empty when unset."),
			"kit_version": computed("The running kit build's version."),
			"license": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The license as of the last check.",
				Attributes: map[string]schema.Attribute{
					installStateAttribute: computed("`absent`, `valid`, `grace`, `expired`, `revoked` or `invalid`."),
					"license_id":          computed("The license's id (`lic-…`); empty unless the license verified."),
					"org":                 computed("The licensed organization's id (`org-…`); empty unless the license verified."),
					installPlanAttribute:  computed("The license's plan, e.g. `FREE`; empty unless the license verified."),
					"exp":                 computed("When the license expires, RFC 3339; null unless it verified."),
					"grace_until":         computed("The expiry plus the license's grace days, RFC 3339; null unless it verified."),
					"checked_at":          computed("When the license was last checked, RFC 3339."),
					"keys": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "The key set the state was decided against; null when none was available.",
						Attributes: map[string]schema.Attribute{
							"source":                  computed("`embedded` (built into this release), `file` (`license.keys`) or `fetched` (from the portal)."),
							installUpdatedAtAttribute: computed("The set's own update time, RFC 3339; the newest set is used."),
						},
					},
				},
			},
			"registration": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Registration with the portal that issued the license.",
				Attributes: map[string]schema.Attribute{
					settingsEnabledAttribute: schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the install registers: on when a license is set, unless `install.register` is false."},
					"last_attempt_at":        computed("When registration was last attempted, RFC 3339; null if never."),
					"last_result":            computed("`ok`, or why the last attempt failed or was not sent; empty if never attempted."),
					"payload": computed("Exactly the JSON the install sends, or would send, to the portal. " +
						"An install that cannot reach the portal uploads this verbatim, so an `output` of it is the offline registration."),
				},
			},
		},
	}
}

func (d *installStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *installStatusDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {

	if d.client == nil {
		resp.Diagnostics.AddError("install_status data source not configured", "Configure was not called with tf.ProviderData")
		return
	}

	out, err := d.client.GetInstallStatus(ctx, &identitypb.GetInstallStatusRequest{Parent: installStatusParent})
	if err != nil {
		resp.Diagnostics.AddError("reading the install status failed", installStatusError(err))
		return
	}

	var m installStatusModel
	resp.Diagnostics.Append(m.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// installStatusError names the cause of the two refusals an operator is
// likely to meet. kit lists no feature for this call, so a kit that does
// not serve it is known only by Unimplemented.
func installStatusError(err error) string {
	if status.Code(err) == codes.Unimplemented {
		return "this kit does not serve the install status; it needs kit 1.94.0 or later. (" + err.Error() + ")"
	}
	if status.Code(err) == codes.PermissionDenied {
		return "only the install's operators may read the install status (identity.installStatus.get, granted at the root). (" + err.Error() + ")"
	}
	return err.Error()
}

func (m *installStatusModel) fromProto(ctx context.Context, out *identitypb.InstallStatus) diag.Diagnostics {

	var diags diag.Diagnostics

	m.Name = types.StringValue(out.GetName())
	m.ID = types.StringValue(out.GetId())
	m.URL = types.StringValue(out.GetUrl())
	m.KitVersion = types.StringValue(out.GetKitVersion())

	keys := types.ObjectNull(installLicenseKeysAttrTypes())
	if k := out.GetLicense().GetKeys(); k != nil {
		var d diag.Diagnostics
		keys, d = types.ObjectValueFrom(ctx, installLicenseKeysAttrTypes(), installLicenseKeysModel{
			Source:    types.StringValue(k.GetSource()),
			UpdatedAt: timestampString(k.GetUpdatedAt()),
		})
		diags.Append(d...)
	}

	l := out.GetLicense()
	license, d := types.ObjectValueFrom(ctx, installLicenseAttrTypes(), installLicenseModel{
		State:      types.StringValue(l.GetState()),
		LicenseID:  types.StringValue(l.GetLicenseId()),
		Org:        types.StringValue(l.GetOrg()),
		Plan:       types.StringValue(l.GetPlan()),
		Exp:        timestampString(l.GetExp()),
		GraceUntil: timestampString(l.GetGraceUntil()),
		CheckedAt:  timestampString(l.GetCheckedAt()),
		Keys:       keys,
	})
	diags.Append(d...)
	m.License = license

	r := out.GetRegistration()
	registration, d := types.ObjectValueFrom(ctx, installRegistrationAttrTypes(), installRegistrationModel{
		Enabled:       types.BoolValue(r.GetEnabled()),
		LastAttemptAt: timestampString(r.GetLastAttemptAt()),
		LastResult:    types.StringValue(r.GetLastResult()),
		Payload:       types.StringValue(r.GetPayload()),
	})
	diags.Append(d...)
	m.Registration = registration

	return diags
}
