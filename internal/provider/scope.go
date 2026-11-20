package provider

import (
	"context"
	"strings"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// --- authwise_scope: kind's valid values ---

const scopeTypeName = "authwise_scope"

// scopeKinds are the scope kinds kit knows (kit pkg/lib/model, v1.97.0),
// each with what it puts in the token. kit stores another kind with no
// effect, and from its next release refuses it on write, so the provider
// refuses it at plan. A kind kit adds needs a provider release.
var scopeKinds = []struct{ kind, effect string }{
	{"openid", "OpenID Connect sign-in: the scope that makes the token response carry an `id_token`."},
	{"authorization.roles", "the subject's roles."},
	{"authorization.permissions", "the subject's permissions."},
	{"authorization.permissionGroups", "the subject's permission groups (bundles of permissions, not the groups a person is in)."},
	{"authorization.groups", "the `groups` claim: the groups the signed-in person belongs to. A client gets it only by asking for the scope."},
	{"authorization.bindings", "the `authz` claim: the subject's effective Access bindings, resolved when the token is issued."},
}

func scopeKindValues() []string {
	out := make([]string, len(scopeKinds))
	for i, k := range scopeKinds {
		out[i] = k.kind
	}
	return out
}

func scopeKindDescription() string {
	var b strings.Builder
	b.WriteString("What the scope grants. One of:\n")
	for _, k := range scopeKinds {
		b.WriteString("  - `" + k.kind + "`: " + k.effect + "\n")
	}
	b.WriteString("\nAny other value is refused at plan: kit stores an unknown kind with no effect, and refuses it on write from its next release.")
	return b.String()
}

// scopeResource is the generated scope with kind described and checked:
// the generator carries no field descriptions and emits validators for
// enums only, and kind is a string. Every other method forwards, the
// optional interfaces included.
type scopeResource struct {
	inner resource.Resource
}

var (
	_ resource.ResourceWithConfigure      = &scopeResource{}
	_ resource.ResourceWithValidateConfig = &scopeResource{}
	_ resource.ResourceWithModifyPlan     = &scopeResource{}
	_ resource.ResourceWithImportState    = &scopeResource{}
)

func newScopeResource() resource.Resource {
	return &scopeResource{inner: generated.NewScopeResource()}
}

func (r *scopeResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *scopeResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *scopeResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if inner, ok := r.inner.(resource.ResourceWithValidateConfig); ok {
		inner.ValidateConfig(ctx, req, resp)
	}
}

func (r *scopeResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if inner, ok := r.inner.(resource.ResourceWithModifyPlan); ok {
		inner.ModifyPlan(ctx, req, resp)
	}
}

func (r *scopeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *scopeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *scopeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *scopeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *scopeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

func (r *scopeResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	if a, ok := resp.Schema.Attributes["kind"].(schema.StringAttribute); ok {
		a.MarkdownDescription = scopeKindDescription()
		a.Validators = append(a.Validators, stringvalidator.OneOf(scopeKindValues()...))
		resp.Schema.Attributes["kind"] = a
	}
}
