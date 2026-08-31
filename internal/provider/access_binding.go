package provider

import (
	"context"
	"fmt"

	accesspb "git.authwise.com/authwise/api-client-go/authwise/access/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// accessBindingTypeName is the Terraform type the wrapper substitutes for.
const accessBindingTypeName = "authwise_access_binding"

// accessRoleCollection is the AIP collection an access role's name sits in.
const accessRoleCollection = "access-roles"

var (
	_ resource.Resource                = &accessBindingResource{}
	_ resource.ResourceWithConfigure   = &accessBindingResource{}
	_ resource.ResourceWithImportState = &accessBindingResource{}
)

// accessBindingResource wraps the generated authwise_access_binding resource
// with the reference check kit does not perform on this write path: a binding
// naming an access role that does not exist in its audience is accepted and
// silently grants nothing (kit#296). Terraform makes that worse than it is
// for a script — the apply succeeds, state records the binding, and every
// later plan is clean, so the grant looks correct forever.
//
// Everything else delegates to the generated resource; only Create and Update
// (role_name is patchable) gain the check.
type accessBindingResource struct {
	inner    resource.Resource
	client   accesspb.AuthwiseAccessServiceClient
	defaults map[string]string
}

// newAccessBindingResource returns the validating wrapper around the
// generated resource.
func newAccessBindingResource() resource.Resource {
	return &accessBindingResource{inner: generated.NewAccessBindingResource()}
}

func (r *accessBindingResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *accessBindingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

// Configure captures the access client and the provider's scope defaults for
// the role lookup, then hands provider data to the generated resource.
func (r *accessBindingResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {

	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		if client, ok := pd.Clients["access"].(accesspb.AuthwiseAccessServiceClient); ok {
			r.client = client
		}
		r.defaults = pd.Defaults
	}

	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *accessBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	r.validateRole(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.inner.Create(ctx, req, resp)
}

func (r *accessBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *accessBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	r.validateRole(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.inner.Update(ctx, req, resp)
}

func (r *accessBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *accessBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

// validateRole resolves the planned role_name in the binding's own audience
// and fails the apply when it does not exist. A parent that cannot be composed
// is left alone: the generated runtime reports that with its own actionable
// diagnostic naming the missing scope attribute.
func (r *accessBindingResource) validateRole(ctx context.Context, plan tfsdk.Plan, diags *diag.Diagnostics) {

	if r.client == nil {
		return
	}

	var roleName types.String
	diags.Append(plan.GetAttribute(ctx, path.Root("role_name"), &roleName)...)
	if diags.HasError() || roleName.IsNull() || roleName.IsUnknown() || roleName.ValueString() == "" {
		return
	}

	parent, err := r.bindingParent(ctx, plan, diags)
	if diags.HasError() || err != nil {
		return
	}

	name := parent + "/" + accessRoleCollection + "/" + roleName.ValueString()

	if _, err := r.client.GetAccessRole(ctx, &accesspb.GetAccessRoleRequest{Name: name}); err != nil {
		if aip.IsNotFound(err) {
			diags.AddAttributeError(path.Root("role_name"),
				"access role not found in this audience",
				fmt.Sprintf("no access role %q exists at %s.\n\n"+
					"The API accepts a binding naming a role that does not exist in its audience and stores it, "+
					"but the binding then grants nothing and nothing about it says so (kit#296). "+
					"Create the role in this audience first, or correct role_name.",
					roleName.ValueString(), name))
			return
		}
		diags.AddAttributeError(path.Root("role_name"),
			"cannot verify the access role", err.Error())
	}
}

// bindingParent composes the binding's audience parent from its scope
// attributes over the provider defaults, the way the generated runtime does.
func (r *accessBindingResource) bindingParent(ctx context.Context, plan tfsdk.Plan, diags *diag.Diagnostics) (string, error) {

	scope := aip.NewScope("tenants", "issuers", "audiences")
	ids := map[string]string{}

	for _, attr := range scope.IdentifierAttributes() {
		ids[attr] = r.defaults[attr]

		var v types.String
		diags.Append(plan.GetAttribute(ctx, path.Root(attr), &v)...)
		if diags.HasError() {
			return "", nil
		}
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
			ids[attr] = v.ValueString()
		}
	}

	return scope.ComposeParent(ids)
}
