package provider

import (
	"context"
	"encoding/json"
	"fmt"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/encoding/protojson"
)

// realmTypeName is the Terraform type the wrapper substitutes for.
const realmTypeName = "authwise_realm"

// authenticationKey is RealmConfig.authentication's protojson name.
const authenticationKey = "authentication"

var realmConfigPath = path.Root("config")

var (
	_ resource.Resource                   = &realmResource{}
	_ resource.ResourceWithConfigure      = &realmResource{}
	_ resource.ResourceWithImportState    = &realmResource{}
	_ resource.ResourceWithValidateConfig = &realmResource{}
)

// realmResource wraps the generated authwise_realm so that it leaves
// RealmConfig.authentication to authwise_realm_authentication_policy.
//
// The generated resource treats config as one JSON document: it reads the
// whole of it into state, and any change patches the whole of it. With a
// policy managed next to it, both halves of that are wrong — every refresh
// would show the policy as drift in the realm's config, and every realm
// config change would send a config without the policy and erase it. So the
// wrapper strips the policy from what the realm resource reads, puts the
// server's current policy back into what it writes, and refuses an
// `authentication` key in the realm's own config.
type realmResource struct {
	inner  resource.Resource
	client identitypb.AuthwiseIdentityServiceClient
}

func newRealmResource() resource.Resource {
	return &realmResource{inner: generated.NewRealmResource()}
}

func (r *realmResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *realmResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

func (r *realmResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

// ValidateConfig refuses the policy inside the realm's own config: it is
// authwise_realm_authentication_policy's to manage, and the realm resource
// never reads it back, so writing it here could only ever show as drift.
func (r *realmResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var config jsontypes.Normalized
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, realmConfigPath, &config)...)
	if resp.Diagnostics.HasError() || config.IsNull() || config.IsUnknown() {
		return
	}

	doc, err := jsonObject(config.ValueString())
	if err != nil {
		// Not an object: the generated conversion reports it at apply.
		return
	}
	if _, ok := doc[authenticationKey]; ok {
		resp.Diagnostics.AddAttributeError(realmConfigPath,
			"the authentication policy is managed separately",
			"Remove the `authentication` key from the realm's config and declare an authwise_realm_authentication_policy for this realm instead. "+
				"The realm resource leaves that part of the config alone, so the two never overwrite each other.")
	}
}

func (r *realmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx, w := withWarnings(ctx)
	defer w.report(&resp.Diagnostics)
	r.inner.Create(ctx, req, resp)
	stripAuthentication(ctx, &resp.State, &resp.Diagnostics)
}

func (r *realmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
	stripAuthentication(ctx, &resp.State, &resp.Diagnostics)
}

// Update writes the server's current policy back into both sides of the
// generated diff before delegating. Putting it in the plan alone would make
// config differ from state on every update; putting it in both keeps the
// update mask exactly what the practitioner changed, and when config is
// among the changes, the patched config carries the policy it would
// otherwise erase.
func (r *realmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	ctx, w := withWarnings(ctx)
	defer w.report(&resp.Diagnostics)

	if r.client != nil {

		var name types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("name"), &name)...)
		if resp.Diagnostics.HasError() {
			return
		}

		got, err := r.client.GetRealm(ctx, &identitypb.GetRealmRequest{Name: name.ValueString()})
		if err != nil {
			resp.Diagnostics.AddError("reading the realm's authentication policy failed", err.Error())
			return
		}

		if policy := got.GetConfig().GetAuthentication(); policy != nil {
			b, err := protojson.Marshal(policy)
			if err != nil {
				resp.Diagnostics.AddError("cannot encode the realm's authentication policy", err.Error())
				return
			}
			injectAuthentication(ctx, &req.Plan, b, &resp.Diagnostics)
			injectAuthenticationState(ctx, &req.State, b, &resp.Diagnostics)
			if resp.Diagnostics.HasError() {
				return
			}
		}
	}

	r.inner.Update(ctx, req, resp)
	stripAuthentication(ctx, &resp.State, &resp.Diagnostics)
}

func (r *realmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *realmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

// stripAuthentication removes the policy from the config in state. A config
// that held nothing else reads as "{}" rather than null: a practitioner who
// wrote `config = jsonencode({})` planned exactly that, and null would be an
// inconsistent result after apply.
func stripAuthentication(ctx context.Context, state *tfsdk.State, diags *diag.Diagnostics) {

	if diags.HasError() || state.Raw.IsNull() {
		return
	}

	var config jsontypes.Normalized
	diags.Append(state.GetAttribute(ctx, realmConfigPath, &config)...)
	if diags.HasError() || config.IsNull() || config.IsUnknown() {
		return
	}

	doc, err := jsonObject(config.ValueString())
	if err != nil {
		return
	}
	if _, ok := doc[authenticationKey]; !ok {
		return
	}
	delete(doc, authenticationKey)

	b, err := json.Marshal(doc)
	if err != nil {
		diags.AddError("cannot encode the realm config", err.Error())
		return
	}

	diags.Append(state.SetAttribute(ctx, realmConfigPath, jsontypes.NewNormalizedValue(string(b)))...)
}

// withAuthentication returns config with the policy set under
// authenticationKey; a null config becomes a document holding only it.
func withAuthentication(config jsontypes.Normalized, policy []byte) (jsontypes.Normalized, error) {

	doc := map[string]json.RawMessage{}
	if !config.IsNull() && !config.IsUnknown() {
		var err error
		if doc, err = jsonObject(config.ValueString()); err != nil {
			return config, err
		}
	}
	doc[authenticationKey] = policy

	b, err := json.Marshal(doc)
	if err != nil {
		return config, err
	}

	return jsontypes.NewNormalizedValue(string(b)), nil
}

func injectAuthentication(ctx context.Context, plan *tfsdk.Plan, policy []byte, diags *diag.Diagnostics) {

	var config jsontypes.Normalized
	diags.Append(plan.GetAttribute(ctx, realmConfigPath, &config)...)
	if diags.HasError() || config.IsUnknown() {
		return
	}

	out, err := withAuthentication(config, policy)
	if err != nil {
		// Not an object: the generated conversion reports it.
		return
	}
	diags.Append(plan.SetAttribute(ctx, realmConfigPath, out)...)
}

func injectAuthenticationState(ctx context.Context, state *tfsdk.State, policy []byte, diags *diag.Diagnostics) {

	var config jsontypes.Normalized
	diags.Append(state.GetAttribute(ctx, realmConfigPath, &config)...)
	if diags.HasError() || config.IsUnknown() {
		return
	}

	out, err := withAuthentication(config, policy)
	if err != nil {
		return
	}
	diags.Append(state.SetAttribute(ctx, realmConfigPath, out)...)
}

func jsonObject(s string) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return nil, fmt.Errorf("config is not a JSON object: %w", err)
	}
	return doc, nil
}
