package provider

import (
	"context"
	"fmt"
	"slices"

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	// attrKeySourceNode is TI7's node that reports the issuer's keys.
	attrKeySourceNode = "key_source_node"

	// attrName and attrDescription are the Guard GFK resources' own
	// attributes of those names.
	attrName        = "name"
	attrDescription = "description"

	keysMissingSummary = "the issuer needs its keys"
	keysMissingDetail  = "Give discover = true to read the issuer's keys, jwks to upload them, or key_source_node to have a node report them (an issuer guard-control cannot reach). " +
		"Only removing a key_source_node may leave none: the issuer keeps the keys last reported."
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
// (authwise_guard_trusted_issuer_rule, generated). key_source_node (TI7,
// guard-control v0.32.0) is a third key source beside discover and jwks;
// the held keys it may leave are read only, adopted in the console or
// awctl.
//
// Hand-written because its keys are a oneof (discover, or an uploaded
// JWKS), which tfinfra does not generate. An issuer is never deleted, only
// revoked: destroy revokes, and a revoked issuer drops out of state at
// refresh, so the next apply registers it afresh. A revoked issuer keeps
// holding its display name (guard-control v0.32.0), so a replacement or a
// new registration needs a new one: the plan warns on a replacement, and
// the create error says so.
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
	KeySourceNode types.String `tfsdk:"key_source_node"`
	HeldKeyIDs    types.List   `tfsdk:"held_key_ids"`
	KeysHeldAt    types.String `tfsdk:"keys_held_at"`
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
			"The keys come from one of three places. `discover = true` reads the issuer's OIDC discovery document and keys, and reads them again as they rotate. " +
			"`jwks` uploads the keys, for an issuer guard-control cannot reach. " +
			"The upload is the keys until it is replaced, so when the issuer rotates its keys, change `jwks` and apply. " +
			"`key_source_node` names a node of the network that reads the keys where it runs and reports them, so an issuer guard-control cannot reach stays current with no upload (guard-control v0.32.0 or later).\n\n" +
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
				MarkdownDescription: "Unique in the network, at most 64 characters. A revoked issuer keeps holding its name, so a change that replaces the issuer (`issuer`, `network_id`, `tenant_id`, or a move from `discover` to `key_source_node`) needs a new `display_name` in the same change; the plan warns when it is kept.",
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
			attrKeySourceNode: schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "A node of the network that reports the issuer's keys, by full name (`authwise_guard_node.<name>.name`), for an issuer guard-control cannot reach: the node reads the keys where it runs and sends them on its own connection, so they stay current with no upload. " +
					"The node must be persistent and must not have joined by a rule of this issuer. Give this, `discover` or `jwks`: they are exclusive here, because with a key source **the keys come from that node's reports**, and an uploaded `jwks` would replace them on every change. " +
					"Without other keys the issuer starts empty, and the node's first report supplies them. Changing it is made in place; removing it stops the reports and keeps the keys last reported, with no other key source needed, and drops a held report. " +
					"An issuer that discovers its keys cannot take a key source: moving from `discover` to `key_source_node` replaces the issuer. " +
					"A report that would remove every key the issuer holds is held (`held_key_ids`) for an administrator to adopt in the console or with awctl. Needs guard-control v0.32.0 or later.",
			},
			"held_key_ids": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The key ids of a report the key source node sent that would have removed every key the issuer holds, held for an administrator; empty when nothing is held. Read only: adopt or drop it in the console or with awctl.",
			},
			"keys_held_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the held report arrived, RFC 3339; null when nothing is held.",
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
	var jwks, source types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrDiscover), &discover)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrJWKS), &jwks)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrKeySourceNode), &source)...)
	if resp.Diagnostics.HasError() || discover.IsUnknown() || jwks.IsUnknown() {
		return
	}

	if at, summary, detail, bad := keysConflict(discover, jwks, source); bad {
		resp.Diagnostics.AddAttributeError(path.Root(at), summary, detail)
	}
}

// keysConflict holds the key sources: exactly one of discover = true, jwks
// and key_source_node. It names the attribute at fault.
func keysConflict(discover types.Bool, jwks, source types.String) (at, summary, detail string, bad bool) {
	if !source.IsNull() {
		switch {
		case discover.ValueBool():
			return attrKeySourceNode, "discover and key_source_node are exclusive",
				"An issuer that discovers its keys takes no key source. Give one of discover, jwks and key_source_node.", true
		case !jwks.IsNull():
			return attrJWKS, "jwks and key_source_node are exclusive",
				"With a key source the keys come from that node's reports; an uploaded jwks would replace them. Give one of discover, jwks and key_source_node.", true
		}
		// A key source alone: the issuer may start with no keys.
		return "", "", "", false
	}
	switch {
	case discover.ValueBool() && !jwks.IsNull():
		return attrJWKS, "discover and jwks are exclusive",
			"Give discover = true to read the issuer's keys, or jwks to upload them, not both.", true
	case !discover.IsNull() && !discover.ValueBool():
		return attrDiscover, "discover = false is not a choice",
			"Leave discover out and give jwks to upload the issuer's keys.", true
	}
	// None of the three is the plan's to judge: a new issuer needs its keys,
	// an issuer whose key source is removed keeps those last reported.
	return "", "", "", false
}

// keysMissing is a plan that gives none of discover = true, jwks and
// key_source_node where the issuer could not keep keys of its own: a new
// issuer, or one that discovered or uploaded them. Removing a key source
// keeps the keys last reported, and an issuer already without a source in
// state (one whose source was removed, or an import) has nothing to drop.
func keysMissing(plan, state *guardTrustedIssuerModel) bool {
	// An unknown value is not null: it may yet give the keys.
	if !plan.Discover.IsNull() || !plan.JWKS.IsNull() || !plan.KeySourceNode.IsNull() {
		return false
	}
	if state == nil {
		return true
	}
	return state.KeySourceNode.IsNull() && (state.Discover.ValueBool() || !state.JWKS.IsNull())
}

// replacesKeepingName is a replacement that keeps the display name: the
// revoked issuer keeps holding it, so guard-control refuses the new one.
func replacesKeepingName(plan, state *guardTrustedIssuerModel) bool {
	replaced := !plan.Issuer.Equal(state.Issuer) || !plan.NetworkID.Equal(state.NetworkID) ||
		!plan.TenantID.Equal(state.TenantID) || discoverToSource(plan, state)
	return replaced && plan.DisplayName.Equal(state.DisplayName)
}

// ModifyPlan refuses a new issuer with no keys and judges an update's keys
// (planKeys). It runs at plan, not validation, because whether an issuer
// may give none depends on its state.
func (r *guardTrustedIssuerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {

	if req.Plan.Raw.IsNull() {
		return
	}

	var plan guardTrustedIssuerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.State.Raw.IsNull() {
		if keysMissing(&plan, nil) {
			resp.Diagnostics.AddAttributeError(path.Root(attrDiscover), keysMissingSummary, keysMissingDetail)
		}
		return
	}

	var state guardTrustedIssuerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planKeys(ctx, &plan, &state, resp)
}

// planKeys judges an update's keys: none where the issuer cannot keep
// its own, a replacement whose name the revoked issuer still holds, a move
// from discovery to a key source (a new issuer), and key_ids unknown when
// the keys change.
func planKeys(ctx context.Context, plan, state *guardTrustedIssuerModel, resp *resource.ModifyPlanResponse) {

	if keysMissing(plan, state) {
		resp.Diagnostics.AddAttributeError(path.Root(attrDiscover), keysMissingSummary, keysMissingDetail)
		return
	}

	// guard-control stops discovering only on an upload, so a move from
	// discover to a key source is a new issuer.
	if discoverToSource(plan, state) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root(attrKeySourceNode))
	}
	if replacesKeepingName(plan, state) {
		resp.Diagnostics.AddAttributeWarning(path.Root(settingsDisplayNameAttribute), "the replacement keeps the display name",
			"This change replaces the issuer: the old one is revoked and a new one registered. A revoked issuer keeps holding its display_name in the network, "+
				"so guard-control refuses the new issuer with AlreadyExists. Give the issuer a new display_name in the same change.")
	}

	if !keysChange(plan, state) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("key_ids"), types.ListUnknown(types.StringType))...)
}

// discoverToSource is a move from discovery to a key source, with no upload
// between: guard-control stops discovering only on an upload.
func discoverToSource(plan, state *guardTrustedIssuerModel) bool {
	return state.Discover.ValueBool() && !plan.Discover.ValueBool() && plan.JWKS.IsNull() && !plan.KeySourceNode.IsNull()
}

// keysChange is whether the plan changes where the keys come from.
func keysChange(plan, state *guardTrustedIssuerModel) bool {
	return !plan.Discover.Equal(state.Discover) || !plan.JWKS.Equal(state.JWKS) || !plan.KeySourceNode.Equal(state.KeySourceNode)
}

func (m *guardTrustedIssuerModel) toProto() *guardpb.TrustedIssuer {
	t := &guardpb.TrustedIssuer{
		DisplayName:   m.DisplayName.ValueString(),
		Description:   m.Description.ValueString(),
		Issuer:        m.Issuer.ValueString(),
		KeySourceNode: m.KeySourceNode.ValueString(),
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
	m.KeySourceNode = optionalString(t.GetKeySourceNode())
	held := make([]attr.Value, 0, len(t.GetHeldKeyIds()))
	for _, k := range t.GetHeldKeyIds() {
		held = append(held, types.StringValue(k))
	}
	heldList, d := types.ListValue(types.StringType, held)
	diags.Append(d...)
	m.HeldKeyIDs = heldList
	m.KeysHeldAt = timestampString(t.GetKeysHeldAt())
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
		resp.Diagnostics.AddError("registering the trusted issuer failed", nameTakenHint(err, keySourceError(err, !plan.KeySourceNode.IsNull())))
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
	if !m.KeySourceNode.Equal(prior.KeySourceNode) {
		paths = append(paths, attrKeySourceNode)
	}
	return paths
}

// keySourceError names the cause when a guard-control that does not serve
// key sources (before v0.32.0: it answers Unimplemented) refuses a write
// that sets one; any other error is guard-control's own.
func keySourceError(err error, setsSource bool) string {
	if setsSource && status.Code(err) == codes.Unimplemented {
		return "key_source_node needs guard-control v0.32.0 or later, which serves key sources (TI7); this guard-control does not. " +
			"Leave key_source_node out, or upgrade guard-control. (" + err.Error() + ")"
	}
	return err.Error()
}

// nameTakenHint adds to guard-control's AlreadyExists that a revoked
// issuer keeps its display name: the usual cause after a replacement or a
// revocation in the console.
func nameTakenHint(err error, msg string) string {
	if status.Code(err) != codes.AlreadyExists {
		return msg
	}
	return msg + "\n\nA revoked issuer keeps holding its display_name in the network. If this issuer replaces one, or registers again after a revocation, give it a new display_name."
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
		resp.Diagnostics.AddError("updating trusted issuer "+t.GetName()+" failed", keySourceError(err, slices.Contains(mask, attrKeySourceNode) && !plan.KeySourceNode.IsNull()))
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
