package provider

import (
	"context"
	"fmt"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	// guardTrustedIssuerClientKey is GuardTrustedIssuerService unadapted: the
	// issuer itself. Its rules go through the adapted client under
	// "guard_trusted_issuer_rule".
	guardTrustedIssuerClientKey = "guard_trusted_issuer"

	guardTrustedIssuerTypeName = "guard_trusted_issuer"
	trustedIssuersCollection   = "trustedIssuers"

	attrDiscover = "discover"
	attrJWKS     = "jwks"
	attrIssuer   = "issuer"

	// attrName and attrDescription are the Guard GFK resources' own
	// attributes of those names.
	attrName        = "name"
	attrDescription = "description"
)

var (
	_ resource.Resource                   = &guardTrustedIssuerResource{}
	_ resource.ResourceWithConfigure      = &guardTrustedIssuerResource{}
	_ resource.ResourceWithImportState    = &guardTrustedIssuerResource{}
	_ resource.ResourceWithValidateConfig = &guardTrustedIssuerResource{}
	_ resource.ResourceWithModifyPlan     = &guardTrustedIssuerResource{}
)

// guardTrustedIssuerResource is authwise_guard_trusted_issuer (train
// 2026.11.5, TI3; the cluster trust of GFK.1 renamed before it was ever
// published): a token issuer registered on a network, whose signed tokens
// let machines join under the issuer's rules
// (authwise_guard_trusted_issuer_rule, generated). key_source_node and the
// held keys (TI7) are left out until guard-control serves them.
//
// Hand-written because its keys are a oneof (discover, or an uploaded
// JWKS), which tfinfra does not generate. An issuer is never deleted, only
// revoked: destroy revokes, and a revoked issuer drops out of state at
// refresh, so the next apply registers it afresh.
type guardTrustedIssuerResource struct {
	client guardpb.GuardTrustedIssuerServiceClient
}

type guardTrustedIssuerModel struct {
	TenantID      types.String `tfsdk:"tenant_id"`
	NetworkID     types.String `tfsdk:"network_id"`
	ID            types.String `tfsdk:"guard_trusted_issuer_id"`
	Name          types.String `tfsdk:"name"`
	DisplayName   types.String `tfsdk:"display_name"`
	Description   types.String `tfsdk:"description"`
	Issuer        types.String `tfsdk:"issuer"`
	Discover      types.Bool   `tfsdk:"discover"`
	JWKS          types.String `tfsdk:"jwks"`
	Audience      types.String `tfsdk:"audience"`
	KeyIDs        types.List   `tfsdk:"key_ids"`
	KeysUpdatedAt types.String `tfsdk:"keys_updated_at"`
	KeysError     types.String `tfsdk:"keys_error"`
	LastJoinAt    types.String `tfsdk:"last_join_at"`
	CreatedBy     types.String `tfsdk:"created_by"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func newGuardTrustedIssuerResource() resource.Resource {
	return &guardTrustedIssuerResource{}
}

func (r *guardTrustedIssuerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardTrustedIssuerTypeName
}

func (r *guardTrustedIssuerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A token issuer registered on a Guard network: a machine holding a token the issuer signed may join the network with it, under the issuer's rules (`authwise_guard_trusted_issuer_rule`), each naming one subject. " +
			"No secret is stored or handed out. The token is short-lived (at most an hour), carries the network's `audience`, and guard-control checks it against the issuer's signing keys.\n\n" +
			"The keys come from one of two places. `discover = true` reads the issuer's OIDC discovery document and keys, and reads them again as they rotate. " +
			"`jwks` uploads the keys, for an issuer guard-control cannot reach. " +
			"The upload is the keys until it is replaced, so when the issuer rotates its keys, change `jwks` and apply.\n\n" +
			"Two issuers of one network may share an issuer address; their key ids must not overlap, and a join picks the issuer by the token's `kid`.\n\n" +
			"An issuer is never deleted. **Destroy revokes it**: no machine joins through it again, and the nodes that joined stay until they are removed. " +
			"An issuer revoked out of band drops out of state at refresh, and the next apply registers it again as a new issuer. " +
			"`issuer` never changes: a different address is a different issuer.",
		Attributes: map[string]schema.Attribute{
			tenantIDAttribute: schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
			},
			networkIDAttribute: schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
			},
			"guard_trusted_issuer_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The issuer's id, `ti-…`: a rule's `trusted_issuer_id`.",
				PlanModifiers:       keep,
			},
			attrName: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The issuer's full name, `tenants/t-…/networks/n-…/trustedIssuers/ti-…`.",
				PlanModifiers:       keep,
			},
			settingsDisplayNameAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique in the network, at most 64 characters.",
			},
			attrDescription: schema.StringAttribute{
				Optional: true,
			},
			attrIssuer: schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The issuer address, exactly as its tokens carry it in `iss`. " +
					"With `discover` it must be an https URL whose host resolves to a public address. Changing it replaces the resource: the old issuer is revoked and a new one registered.",
				PlanModifiers: replace,
			},
			attrDiscover: schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "`true` to read the issuer's discovery document and its keys. Give this or `jwks`.",
			},
			attrJWKS: schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The issuer's JWKS document, as JSON: at most 64 KiB and 16 keys, each RSA of 2048 bits or more or EC P-256. Give this or `discover`. " +
					"guard-control never returns it: `key_ids` says which keys it holds. After an import it is unknown to Terraform, so the next apply uploads the configured document again.",
			},
			"audience": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The audience a joining machine's token must carry, `guard:tenants/t-…/networks/n-…`: the same for every issuer of the network, so a token for one network joins no other.",
				PlanModifiers:       keep,
			},
			"key_ids": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The `kid` of each key in use.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"keys_updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the keys were last read or uploaded, RFC 3339.",
			},
			"keys_error": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Why the last read of the keys failed; null when it worked.",
			},
			"last_join_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When a machine last joined through the issuer, RFC 3339.",
			},
			"created_by": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: keep,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: keep,
			},
		},
	}
}

func (r *guardTrustedIssuerResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.client, _ = pd.Clients[guardTrustedIssuerClientKey].(guardpb.GuardTrustedIssuerServiceClient)
	}
}

func (r *guardTrustedIssuerResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_guard_trusted_issuer is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

// ValidateConfig holds the oneof: exactly one of discover = true and jwks.
func (r *guardTrustedIssuerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var discover types.Bool
	var jwks types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrDiscover), &discover)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrJWKS), &jwks)...)
	if resp.Diagnostics.HasError() || discover.IsUnknown() || jwks.IsUnknown() {
		return
	}

	switch {
	case discover.ValueBool() && !jwks.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root(attrJWKS), "discover and jwks are exclusive",
			"Give discover = true to read the issuer's keys, or jwks to upload them, not both.")
	case !discover.IsNull() && !discover.ValueBool():
		resp.Diagnostics.AddAttributeError(path.Root(attrDiscover), "discover = false is not a choice",
			"Leave discover out and give jwks to upload the issuer's keys.")
	case discover.IsNull() && jwks.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root(attrDiscover), "the issuer needs its keys",
			"Give discover = true to read the issuer's keys, or jwks to upload them (an issuer guard-control cannot reach).")
	}
}

// ModifyPlan plans key_ids unknown when the keys change: an upload, or a
// move between discovery and an upload. Otherwise they keep their state.
func (r *guardTrustedIssuerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {

	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var plan, state guardTrustedIssuerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Discover.Equal(state.Discover) && plan.JWKS.Equal(state.JWKS) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("key_ids"), types.ListUnknown(types.StringType))...)
}

func (m *guardTrustedIssuerModel) toProto() *guardpb.TrustedIssuer {
	t := &guardpb.TrustedIssuer{
		DisplayName: m.DisplayName.ValueString(),
		Description: m.Description.ValueString(),
		Issuer:      m.Issuer.ValueString(),
	}
	if m.Discover.ValueBool() {
		t.Keys = &guardpb.TrustedIssuer_Discover{Discover: true}
	} else if !m.JWKS.IsNull() {
		t.Keys = &guardpb.TrustedIssuer_Jwks{Jwks: []byte(m.JWKS.ValueString())}
	}
	return t
}

// fromProto reads what guard-control returns. jwks is input only and kept
// as configured; discover is read when the server says so.
func (m *guardTrustedIssuerModel) fromProto(t *guardpb.TrustedIssuer) diag.Diagnostics {

	var diags diag.Diagnostics

	m.Name = types.StringValue(t.GetName())
	if ids, id, err := guardNetworkScope.ParseName(trustedIssuersCollection, t.GetName()); err == nil {
		m.TenantID = types.StringValue(ids[tenantIDAttribute])
		m.NetworkID = types.StringValue(ids[networkIDAttribute])
		m.ID = types.StringValue(id)
	}
	m.DisplayName = types.StringValue(t.GetDisplayName())
	m.Description = optionalString(t.GetDescription())
	m.Issuer = types.StringValue(t.GetIssuer())
	if t.GetDiscover() {
		m.Discover = types.BoolValue(true)
	}
	m.Audience = types.StringValue(t.GetAudience())
	ids := make([]attr.Value, 0, len(t.GetKeyIds()))
	for _, k := range t.GetKeyIds() {
		ids = append(ids, types.StringValue(k))
	}
	list, d := types.ListValue(types.StringType, ids)
	diags.Append(d...)
	m.KeyIDs = list
	m.KeysUpdatedAt = timestampString(t.GetKeysUpdatedAt())
	m.KeysError = optionalString(t.GetKeysError())
	m.LastJoinAt = timestampString(t.GetLastJoinAt())
	m.CreatedBy = types.StringValue(t.GetCreatedBy())
	m.CreatedAt = timestampString(t.GetCreatedAt())

	return diags
}

// optionalString reads an empty proto string as null.
func optionalString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func (r *guardTrustedIssuerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan guardTrustedIssuerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parent, err := guardNetworkScope.ComposeParent(map[string]string{
		tenantIDAttribute:  plan.TenantID.ValueString(),
		networkIDAttribute: plan.NetworkID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("cannot name the network", err.Error())
		return
	}

	out, err := r.client.CreateTrustedIssuer(ctx, &guardpb.CreateTrustedIssuerRequest{Parent: parent, TrustedIssuer: plan.toProto()})
	if err != nil {
		resp.Diagnostics.AddError("registering the trusted issuer failed", err.Error())
		return
	}

	resp.Diagnostics.Append(plan.fromProto(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read drops an issuer that is gone or revoked.
func (r *guardTrustedIssuerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardTrustedIssuerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetTrustedIssuer(ctx, &guardpb.GetTrustedIssuerRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading trusted issuer "+state.Name.ValueString()+" failed", err.Error())
		return
	}
	if out.GetRevokedAt() != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(state.fromProto(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// updateMask names what changed among display_name, description and the
// keys. A change of key source, or a new upload, sends the keys.
func (m *guardTrustedIssuerModel) updateMask(prior *guardTrustedIssuerModel) []string {
	var paths []string
	if !m.DisplayName.Equal(prior.DisplayName) {
		paths = append(paths, settingsDisplayNameAttribute)
	}
	if !m.Description.Equal(prior.Description) {
		paths = append(paths, attrDescription)
	}
	switch {
	case m.Discover.ValueBool() && !prior.Discover.ValueBool():
		paths = append(paths, attrDiscover)
	case !m.JWKS.IsNull() && !m.JWKS.Equal(prior.JWKS):
		paths = append(paths, attrJWKS)
	}
	return paths
}

func (r *guardTrustedIssuerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state guardTrustedIssuerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	t := plan.toProto()
	t.Name = state.Name.ValueString()
	mask := plan.updateMask(&state)

	out, err := r.client.UpdateTrustedIssuer(ctx, &guardpb.UpdateTrustedIssuerRequest{TrustedIssuer: t, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
	if err != nil {
		resp.Diagnostics.AddError("updating trusted issuer "+t.GetName()+" failed", err.Error())
		return
	}

	resp.Diagnostics.Append(plan.fromProto(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete revokes. An issuer already gone or revoked is not an error.
func (r *guardTrustedIssuerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardTrustedIssuerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.RevokeTrustedIssuer(ctx, &guardpb.RevokeTrustedIssuerRequest{Name: state.Name.ValueString()}); err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("revoking trusted issuer "+state.Name.ValueString()+" failed", err.Error())
	}
}

// ImportState takes the issuer's full name.
func (r *guardTrustedIssuerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, id, err := guardNetworkScope.ParseName(trustedIssuersCollection, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("not a trusted issuer's name",
			fmt.Sprintf("import takes tenants/t-…/networks/n-…/trustedIssuers/ti-…: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(attrName), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), ids[tenantIDAttribute])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(networkIDAttribute), ids[networkIDAttribute])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("guard_trusted_issuer_id"), id)...)
}
