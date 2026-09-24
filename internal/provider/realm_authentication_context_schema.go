package provider

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &realmAuthenticationContextSchemaDataSource{}
	_ datasource.DataSourceWithConfigure = &realmAuthenticationContextSchemaDataSource{}
)

// realmAuthenticationContextSchemaDataSource reads what a realm's
// authentication rules can be written against: the CEL variables a
// condition may read, the factor types this kit build runs, and the
// warnings kit has about the policy the realm holds now. It pairs with
// authwise_realm_authentication_policy — a check block over `warnings`
// fails a plan on a policy kit doubts, and `factor_types` says which
// factor_type values an authwise_factor may take.
type realmAuthenticationContextSchemaDataSource struct {
	client identitypb.AuthwiseIdentityServiceClient
}

type realmAuthenticationContextSchemaModel struct {
	Realm       types.String `tfsdk:"realm"`
	Variables   types.List   `tfsdk:"variables"`
	FactorTypes types.List   `tfsdk:"factor_types"`
	Warnings    types.List   `tfsdk:"warnings"`
}

type authenticationContextVariableModel struct {
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
	Description types.String `tfsdk:"description"`
}

type authenticationFactorTypeModel struct {
	FactorType         types.String `tfsdk:"factor_type"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	Classes            types.List   `tfsdk:"classes"`
	PhishingResistant  types.Bool   `tfsdk:"phishing_resistant"`
	Restricted         types.Bool   `tfsdk:"restricted"`
	Recovery           types.Bool   `tfsdk:"recovery"`
	SupportsEnrollment types.Bool   `tfsdk:"supports_enrollment"`
	Amr                types.List   `tfsdk:"amr"`
	ConfigMessage      types.String `tfsdk:"config_message"`
}

func authenticationContextVariableAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":        types.StringType,
		"type":        types.StringType,
		"description": types.StringType,
	}
}

func authenticationFactorTypeAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"factor_type":         types.StringType,
		"enabled":             types.BoolType,
		"classes":             types.ListType{ElemType: types.StringType},
		"phishing_resistant":  types.BoolType,
		"restricted":          types.BoolType,
		"recovery":            types.BoolType,
		"supports_enrollment": types.BoolType,
		"amr":                 types.ListType{ElemType: types.StringType},
		"config_message":      types.StringType,
	}
}

func newRealmAuthenticationContextSchemaDataSource() datasource.DataSource {
	return &realmAuthenticationContextSchemaDataSource{}
}

func (d *realmAuthenticationContextSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_realm_authentication_context_schema"
}

func (d *realmAuthenticationContextSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {

	strings := schema.ListAttribute{Computed: true, ElementType: types.StringType}

	resp.Schema = schema.Schema{
		MarkdownDescription: "What a realm's authentication rules can be written against: the variables a rule `condition` may read, the factor types this kit build runs, " +
			"and kit's warnings about the policy the realm holds now — the same warnings a write returns. " +
			"A `check` block over `warnings` surfaces a doubtful policy on every plan, not only on the apply that wrote it.",
		Attributes: map[string]schema.Attribute{
			"realm": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Full resource name of the realm (`tenants/{t}/realms/{r}`).",
			},
			"variables": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "What a rule condition may read, in kit's documented order.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "The CEL path, e.g. `risk.level`."},
					"type":        schema.StringAttribute{Computed: true, MarkdownDescription: "The CEL type, e.g. `string`, `list(string)`."},
					"description": schema.StringAttribute{Computed: true},
				}},
			},
			"factor_types": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Every factor type kit knows, and whether this build runs it.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"factor_type":         schema.StringAttribute{Computed: true, MarkdownDescription: "The slug an `authwise_factor` and a rule name."},
					"enabled":             schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this build runs it; kit refuses a factor of a type it does not."},
					"classes":             strings,
					"phishing_resistant":  schema.BoolAttribute{Computed: true},
					"restricted":          schema.BoolAttribute{Computed: true, MarkdownDescription: "NIST's restricted class (SMS): offered only beside an unrestricted method."},
					"recovery":            schema.BoolAttribute{Computed: true, MarkdownDescription: "A look-up secret; never the method a requirement is met by."},
					"supports_enrollment": schema.BoolAttribute{Computed: true, MarkdownDescription: "False where enrolment happens elsewhere (Duo)."},
					"amr":                 strings,
					"config_message":      schema.StringAttribute{Computed: true, MarkdownDescription: "The message the factor's `config` carries for this type; empty for none."},
				}},
			},
			"warnings": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The realm's stored policy judged against its enabled factors. Empty when kit has nothing to say.",
			},
		},
	}
}

func (d *realmAuthenticationContextSchemaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *realmAuthenticationContextSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if d.client == nil {
		resp.Diagnostics.AddError("realm_authentication_context_schema data source not configured", "Configure was not called with tf.ProviderData")
		return
	}

	var m realmAuthenticationContextSchemaModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := d.client.GetRealmAuthenticationContextSchema(ctx, &identitypb.GetRealmAuthenticationContextSchemaRequest{Name: m.Realm.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("reading the realm's authentication context schema failed", err.Error())
		return
	}

	resp.Diagnostics.Append(m.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// fromProto fills the computed attributes. Lists are always known and
// empty rather than null, so `length(...warnings) == 0` works in a check.
func (m *realmAuthenticationContextSchemaModel) fromProto(ctx context.Context, out *identitypb.RealmAuthenticationContextSchema) diag.Diagnostics {

	var diags diag.Diagnostics

	stringList := func(v []string) types.List {
		l, d := types.ListValueFrom(ctx, types.StringType, append([]string{}, v...))
		diags.Append(d...)
		return l
	}

	variables := make([]authenticationContextVariableModel, 0, len(out.GetVariables()))
	for _, v := range out.GetVariables() {
		variables = append(variables, authenticationContextVariableModel{
			Name:        types.StringValue(v.GetName()),
			Type:        types.StringValue(v.GetType()),
			Description: types.StringValue(v.GetDescription()),
		})
	}

	factorTypes := make([]authenticationFactorTypeModel, 0, len(out.GetFactorTypes()))
	for _, f := range out.GetFactorTypes() {
		factorTypes = append(factorTypes, authenticationFactorTypeModel{
			FactorType:         types.StringValue(f.GetFactorType()),
			Enabled:            types.BoolValue(f.GetEnabled()),
			Classes:            stringList(f.GetClasses()),
			PhishingResistant:  types.BoolValue(f.GetPhishingResistant()),
			Restricted:         types.BoolValue(f.GetRestricted()),
			Recovery:           types.BoolValue(f.GetRecovery()),
			SupportsEnrollment: types.BoolValue(f.GetSupportsEnrollment()),
			Amr:                stringList(f.GetAmr()),
			ConfigMessage:      types.StringValue(f.GetConfigMessage()),
		})
	}

	var d diag.Diagnostics
	m.Variables, d = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: authenticationContextVariableAttrTypes()}, variables)
	diags.Append(d...)
	m.FactorTypes, d = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: authenticationFactorTypeAttrTypes()}, factorTypes)
	diags.Append(d...)
	m.Warnings = stringList(out.GetWarnings())

	return diags
}
