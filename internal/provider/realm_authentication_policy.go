package provider

import (
	"context"
	"fmt"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// authenticationPolicyPath is the update_mask path the policy owns inside
// the realm. kit merges a nested path on its own, so the patch touches
// nothing else in RealmConfig.
const authenticationPolicyPath = "config.authentication"

// realmAttribute is the policy resource's own attribute: the realm it
// governs. Everything else is derived from AuthenticationPolicy.
const realmAttribute = "realm"

var (
	_ resource.Resource                = &realmAuthenticationPolicyResource{}
	_ resource.ResourceWithConfigure   = &realmAuthenticationPolicyResource{}
	_ resource.ResourceWithImportState = &realmAuthenticationPolicyResource{}
)

// realmAuthenticationPolicyResource manages RealmConfig.authentication as a
// resource of its own, so it does not fight authwise_realm over one config
// blob: the realm resource leaves the subtree alone (see realmResource) and
// this one writes only that subtree, through PatchRealm with a nested mask.
//
// The policy is authoritative. Create overwrites whatever policy the realm
// had, and destroy clears it, leaving the realm on kit's defaults.
type realmAuthenticationPolicyResource struct {
	client identitypb.AuthwiseIdentityServiceClient
}

func newRealmAuthenticationPolicyResource() resource.Resource {
	return &realmAuthenticationPolicyResource{}
}

func (r *realmAuthenticationPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_realm_authentication_policy"
}

// authenticationPolicyDescriptions documents the fields whose meaning is not
// obvious from the name, keyed by dotted path within AuthenticationPolicy.
var authenticationPolicyDescriptions = func() map[string]string {

	requirement := map[string]string{
		"mode":                   "What must hold beyond the primary: `NONE`, `ANY_FACTOR`, `TYPES`, `PHISHING_RESISTANT` or `DENY` (never valid as the floor).",
		"min_factors":            "Distinct factor classes required beyond the primary's own. 0 means the mode's default, which for `ANY_FACTOR` is 1.",
		"allowed_factor_types":   "Factor types that may count. Empty means every factor enabled on the realm.",
		"required_factor_types":  "Factor types that must all be present (mode `TYPES`).",
		"reauth_after":           "A factor verified longer ago than this does not count. Unset falls back to `session.factor_reauth`.",
		"skip_if_device_trusted": "Whether a trusted device satisfies the requirement without a factor. Never honoured for `PHISHING_RESISTANT` or `DENY`.",
		"deny_reason":            "Mode `DENY` only. Recorded in the audit trail, never shown to the person.",
		"hardware_bound":         "With `PHISHING_RESISTANT`: the authenticator must not be backup-eligible.",
	}

	d := map[string]string{
		"rules":                           "Rules, evaluated in order; the first whose condition holds decides. A realm with no rules behaves as one rule `true` requiring the floor.",
		"rules.name":                      "The rule's name, recorded on the login event. kit refuses an unnamed rule.",
		"rules.condition":                 "CEL over the authentication context. Empty or `true` always matches. kit checks it on write, so a bad expression fails the apply with kit's reason.",
		"rules.require":                   "What the rule requires; raised to the floor.",
		"floor":                           "The least any rule may require: every outcome is raised to it, so a misjudged rule can only ask for more.",
		"enrollment":                      "Setting this block at all switches kit from its defaults to the values here, `in_flow` included: kit reads an absent enrollment policy as in-flow enrollment on, and a present one at face value.",
		"enrollment.in_flow":              "Whether a person may enrol a factor inside the login flow when a rule demands one they lack.",
		"enrollment.self_service_types":   "Factor types a person may add from the account page. Empty means every factor enabled on the realm.",
		"enrollment.offer_recovery_codes": "Whether recovery codes are offered after the first factor is enrolled.",
		"acr_levels":                      "The acr vocabulary this realm can emit, weakest first: the last satisfied entry is the emitted acr.",
		"risk.external_endpoint_name":     "The endpoint (`tenants/{t}/endpoints/{e}`) running AuthwiseRiskService, for modes `EXTERNAL` and `BOTH`.",
	}

	for k, v := range requirement {
		d["floor."+k] = v
		d["rules.require."+k] = v
		d["acr_levels.require."+k] = v
	}

	return d
}()

func (r *realmAuthenticationPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	attrs := protoAttributes((&corepb.AuthenticationPolicy{}).ProtoReflect().Descriptor(), authenticationPolicyDescriptions)

	attrs[realmAttribute] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "Full resource name of the realm the policy governs (`tenants/{t}/realms/{r}`), e.g. `authwise_realm.x.name`. Changing it moves the policy: the old realm's is cleared and the new realm's is written.",
		PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A realm's authentication policy: the rules, the floor, enrollment, remembered devices, session, throttle, risk and the acr levels (`RealmConfig.authentication`).\n\n" +
			"It is managed apart from `authwise_realm` so the two do not fight over the realm's config: the realm resource never reads or writes this part of it, and does not accept an `authentication` key in its own `config`. " +
			"The policy is authoritative — it replaces whatever policy the realm had — and destroying it clears the policy, leaving the realm on kit's defaults.",
		Attributes: attrs,
	}
}

func (r *realmAuthenticationPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
}

// identityClientFrom reads the identity client out of provider data; nil
// provider data (before the provider is configured) is not an error.
func identityClientFrom(providerData any, diags *diag.Diagnostics) identitypb.AuthwiseIdentityServiceClient {

	if providerData == nil {
		return nil
	}

	pd, ok := providerData.(*tfruntime.ProviderData)
	if !ok {
		diags.AddError("unexpected provider data", fmt.Sprintf("expected *tf.ProviderData, got %T", providerData))
		return nil
	}

	client, ok := pd.Clients["identity"].(identitypb.AuthwiseIdentityServiceClient)
	if !ok {
		diags.AddError("missing client", `provider data key "identity" is not an AuthwiseIdentityServiceClient`)
		return nil
	}

	return client
}

func (r *realmAuthenticationPolicyResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("realm_authentication_policy resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *realmAuthenticationPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *realmAuthenticationPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// write patches the planned policy into the realm and records the plan as
// state. Every attribute is plain Optional, so the plan is fully known at
// apply and is exactly what was written.
func (r *realmAuthenticationPolicyResource) write(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diags *diag.Diagnostics) {

	if !r.configured(diags) {
		return
	}

	realm, policy := r.fromTerraform(ctx, plan.Raw, plan.GetAttribute, diags)
	if diags.HasError() {
		return
	}

	// kit judges the realm the write leaves and answers with warnings for
	// what it accepted but doubts: a rule that can never fire, a
	// requirement no enabled factor meets.
	ctx, w := withWarnings(ctx)
	defer w.report(diags)

	_, err := r.client.PatchRealm(ctx, &identitypb.PatchRealmRequest{
		Name:       realm,
		Realm:      &corepb.Realm{Config: &corepb.RealmConfig{Authentication: policy}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{authenticationPolicyPath}},
	})
	if err != nil {
		diags.AddError("writing the realm authentication policy failed", err.Error())
		return
	}

	state.Raw = plan.Raw
}

// Read refreshes from the realm. State is kept as it is whenever it converts
// to the policy the server holds: proto3 reads a configured zero value back
// as unset, and rewriting state from the server would turn every explicit
// `min_factors = 0` or `in_flow = false` into drift.
func (r *realmAuthenticationPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	realm, prior := r.fromTerraform(ctx, req.State.Raw, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetRealm(ctx, &identitypb.GetRealmRequest{Name: realm})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the realm authentication policy failed", err.Error())
		return
	}

	server := got.GetConfig().GetAuthentication()
	if server == nil {
		server = &corepb.AuthenticationPolicy{}
	}

	if proto.Equal(prior, server) {
		return
	}

	typ, ok := resp.State.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		resp.Diagnostics.AddError("unexpected schema type", "the policy schema is not an object")
		return
	}

	resp.State.Raw = protoToValue(server.ProtoReflect(), typ)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(realmAttribute), realm)...)
}

// Delete clears the policy. An empty RealmConfig under the nested mask
// removes exactly the authentication subtree; a realm that is already gone
// has no policy left to clear.
func (r *realmAuthenticationPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var realm types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(realmAttribute), &realm)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.PatchRealm(ctx, &identitypb.PatchRealmRequest{
		Name:       realm.ValueString(),
		Realm:      &corepb.Realm{Config: &corepb.RealmConfig{}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{authenticationPolicyPath}},
	})
	if err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("clearing the realm authentication policy failed", err.Error())
	}
}

// ImportState takes the realm's full resource name; the refresh that follows
// reads the policy.
func (r *realmAuthenticationPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root(realmAttribute), req, resp)
}

// fromTerraform reads the realm name and the policy out of a plan or state.
func (r *realmAuthenticationPolicyResource) fromTerraform(
	ctx context.Context,
	raw tftypes.Value,
	getAttribute func(context.Context, path.Path, any) diag.Diagnostics,
	diags *diag.Diagnostics,
) (string, *corepb.AuthenticationPolicy) {

	var realm types.String
	diags.Append(getAttribute(ctx, path.Root(realmAttribute), &realm)...)
	if diags.HasError() {
		return "", nil
	}

	policy := &corepb.AuthenticationPolicy{}
	if err := valueToProto(raw, policy.ProtoReflect()); err != nil {
		diags.AddError("invalid authentication policy", err.Error())
		return "", nil
	}

	return realm.ValueString(), policy
}
