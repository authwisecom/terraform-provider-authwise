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
// each with what it puts in the token. The permissions claim is scoped
// (kit, ruled 2026-11-22): it carries what the subject holds and the
// request asked for, never every held permission by default (tf#84). kit stores another kind with no
// effect, and from its next release refuses it on write, so the provider
// refuses it at plan. A kind kit adds needs a provider release.
var scopeKinds = []struct{ kind, effect string }{
	{"openid", "OpenID Connect sign-in: the scope that makes the token response carry an `id_token`."},
	{"authorization.roles", "the `roles` claim: the subject's roles. Only a scope named `roles` has an effect."},
	{"authorization.permissions", "the `permissions` claim in direct mode: the permissions the subject holds **and** the request names as scope words. Only a scope named `permissions` has an effect."},
	{"authorization.permissionGroups", "the `permissions` claim in groups mode: the permissions the subject holds among those associated to the requested scopes with `authwise_scope_access_permissions`."},
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
	if a, ok := resp.Schema.Attributes["auto"].(schema.BoolAttribute); ok {
		a.MarkdownDescription = scopeAutoDescription
		resp.Schema.Attributes["auto"] = a
	}
}

const scopeAutoDescription = "Grant the scope on every token request for the audience, whether or not the client asks for it. Default false: a client gets the scope only by requesting it."

// --- authwise_scope_access_permissions: what the association does ---

const scopeAccessPermissionsTypeName = "authwise_scope_access_permissions"

// scopeAccessPermissionsNote is appended to the generated description: kit
// reads a scope's associated permissions only for the permission-groups
// kind, and the generator cannot say so (tf#84).
const scopeAccessPermissionsNote = " Has an effect only on a scope of kind `authorization.permissionGroups`: a client that requests that scope gets, in the `permissions` claim, the permissions the subject holds among those associated here. kit ignores the set on a scope of any other kind."

// scopeAccessPermissionsResource is the generated association with its
// description completed. Every other method forwards.
type scopeAccessPermissionsResource struct {
	inner resource.Resource
}

var (
	_ resource.ResourceWithConfigure   = &scopeAccessPermissionsResource{}
	_ resource.ResourceWithImportState = &scopeAccessPermissionsResource{}
)

func newScopeAccessPermissionsResource() resource.Resource {
	return &scopeAccessPermissionsResource{inner: generated.NewScopeAccessPermissionsResource()}
}

func (r *scopeAccessPermissionsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *scopeAccessPermissionsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	resp.Schema.MarkdownDescription += scopeAccessPermissionsNote
}

func (r *scopeAccessPermissionsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *scopeAccessPermissionsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *scopeAccessPermissionsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *scopeAccessPermissionsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *scopeAccessPermissionsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *scopeAccessPermissionsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}
