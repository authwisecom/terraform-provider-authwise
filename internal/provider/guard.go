package provider

import (
	"context"
	"strings"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Guard (guard-control, #32) is a second service behind the
// same provider: its own endpoint, the same bearer. Its resources are
// authwise_guard_*. Without guard_endpoint they fail at plan, and its data
// sources at read, with errGuardNotConfigured.

const (
	// guardClientKey is the ProviderData.Clients key of the guard-control
	// client.
	guardClientKey = "guard"
	// guardConfiguredKey holds whether guard_endpoint was set, so a Guard
	// resource can refuse at plan rather than at apply.
	guardConfiguredKey = "guard_configured"

	guardTypePrefix = providerTypeName + "_guard_"

	networkIDAttribute = "network_id"

	guardTenantIDDescription = "The Guard tenant's id (`gt-…`), as awtenant reports it or `authwise_guard_tenants` lists it; the provider's `tenant_id` is not used."
)

// guardTenantReference validates a Guard tenant's id: Guard's tenants are
// its own since apis v0.24.0 (#35), not kit's.
var guardTenantReference = tfruntime.ReferenceID("gt", "guard_tenant", "")

const errGuardNotConfigured = "guard_endpoint is not configured: set the provider's guard_endpoint attribute or the AUTHWISE_GUARD_ENDPOINT environment variable to guard-control's gRPC host:port"

// unconfiguredGuardConn is the guard-control connection when guard_endpoint
// is unset: every call fails with errGuardNotConfigured.
type unconfiguredGuardConn struct{}

func (unconfiguredGuardConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return status.Error(codes.FailedPrecondition, errGuardNotConfigured)
}

func (unconfiguredGuardConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.FailedPrecondition, errGuardNotConfigured)
}

// guardConnFor is Guard's connection: the same bearer, sent to
// guard-control. Without guard_endpoint it is unconfiguredGuardConn, and
// configured is false.
func guardConnFor(config authwiseProviderModel, dial func(string) (*grpc.ClientConn, error)) (conn grpc.ClientConnInterface, configured bool, err error) {
	endpoint := stringOr(config.GuardEndpoint, "AUTHWISE_GUARD_ENDPOINT")
	if endpoint == "" {
		return unconfiguredGuardConn{}, false, nil
	}
	c, err := dial(endpoint)
	if err != nil {
		return nil, false, err
	}
	return c, true, nil
}

// guardWrap wraps every authwise_guard_* resource, generated or wrapped
// already, in guardResource.
func guardWrap(ctx context.Context, factories []func() resource.Resource) []func() resource.Resource {

	out := make([]func() resource.Resource, 0, len(factories))

	for _, factory := range factories {
		resp := &resource.MetadataResponse{}
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, resp)
		if strings.HasPrefix(resp.TypeName, guardTypePrefix) {
			inner := factory
			factory = func() resource.Resource { return &guardResource{inner: inner()} }
		}
		out = append(out, factory)
	}

	return out
}

// guardResource refuses to plan a Guard resource when guard_endpoint is
// unset. Destroying one is still planned, and fails at apply with the same
// message.
type guardResource struct {
	inner      resource.Resource
	configured bool
}

func (r *guardResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

// Schema makes tenant_id and network_id required where a resource has
// them. tenant_id is a Guard tenant's id (`gt-…`), which the provider's
// tenant_id, a kit tenant's, is not; and the provider has no network
// default.
func (r *guardResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	if a, ok := resp.Schema.Attributes[tenantIDAttribute].(schema.StringAttribute); ok {
		a.Required, a.Optional = true, false
		a.MarkdownDescription = guardTenantIDDescription + " Changing it replaces the resource."
		a.Validators = []validator.String{guardTenantReference}
		resp.Schema.Attributes[tenantIDAttribute] = a
	}
	if a, ok := resp.Schema.Attributes[networkIDAttribute].(schema.StringAttribute); ok {
		a.Required, a.Optional = true, false
		a.MarkdownDescription = "The network's id, `authwise_guard_network.<name>.guard_network_id`. Changing it replaces the resource."
		resp.Schema.Attributes[networkIDAttribute] = a
	}
}

func (r *guardResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.configured, _ = pd.Clients[guardConfiguredKey].(bool)
	} else {
		// Before the provider is configured (validation), nothing is known.
		r.configured = true
	}
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *guardResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !r.configured && !req.Plan.Raw.IsNull() {
		resp.Diagnostics.AddError("Guard is not configured", errGuardNotConfigured)
		return
	}
	if inner, ok := r.inner.(resource.ResourceWithModifyPlan); ok {
		inner.ModifyPlan(ctx, req, resp)
	}
}

func (r *guardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *guardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *guardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *guardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *guardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
		return
	}
	resp.Diagnostics.AddError("import not supported", "this resource cannot be imported")
}

// guardDataSourceWrap wraps every authwise_guard_* data source in
// guardDataSource.
func guardDataSourceWrap(ctx context.Context, factories []func() datasource.DataSource) []func() datasource.DataSource {

	out := make([]func() datasource.DataSource, 0, len(factories))

	for _, factory := range factories {
		resp := &datasource.MetadataResponse{}
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, resp)
		if strings.HasPrefix(resp.TypeName, guardTypePrefix) {
			inner := factory
			factory = func() datasource.DataSource { return &guardDataSource{inner: inner()} }
		}
		out = append(out, factory)
	}

	return out
}

// guardDataSource makes a list's tenant_id required and a Guard tenant's
// id, as guardResource does a resource's.
type guardDataSource struct {
	inner datasource.DataSource
}

func (d *guardDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	d.inner.Metadata(ctx, req, resp)
}

func (d *guardDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	d.inner.Schema(ctx, req, resp)
	if a, ok := resp.Schema.Attributes[tenantIDAttribute].(dsschema.StringAttribute); ok && !a.Computed {
		a.Required, a.Optional = true, false
		a.MarkdownDescription = guardTenantIDDescription
		a.Validators = []validator.String{guardTenantReference}
		resp.Schema.Attributes[tenantIDAttribute] = a
	}
}

func (d *guardDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if inner, ok := d.inner.(datasource.DataSourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (d *guardDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	d.inner.Read(ctx, req, resp)
}

// guardTenantTypeName is the generated Guard tenant resource, which the
// provider does not register: a Guard tenant is created only through
// guard-control's mTLS tenancy listener, by awtenant and the portal worker
// (E22, #36). Its data sources remain.
const guardTenantTypeName = "authwise_guard_tenant"

// The Guard types whose create response alone carries a value: an invite's
// code and join link, and a node's auth code.
const (
	guardInviteTypeName = "authwise_guard_invite"
	guardNodeTypeName   = "authwise_guard_node"
)

func newGuardInviteResource() resource.Resource {
	return &keepOnceResource{inner: generated.NewInviteResource(), attrs: []string{"code", "url"}}
}

func newGuardNodeResource() resource.Resource {
	return &keepOnceResource{inner: generated.NewNodeResource(), attrs: []string{"auth_code"}}
}

// keepOnceResource carries attrs across reads: guard-control returns them
// in the create response and never again, so a refresh would otherwise
// null them. An imported resource has none of them.
type keepOnceResource struct {
	inner resource.Resource
	attrs []string
}

func (r *keepOnceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *keepOnceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

func (r *keepOnceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *keepOnceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *keepOnceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	prior := r.kept(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.inner.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		return
	}

	r.restore(ctx, prior, &resp.State, &resp.Diagnostics)
}

// Update keeps them too: the update response does not carry them either.
func (r *keepOnceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	prior := r.kept(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.inner.Update(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	r.restore(ctx, prior, &resp.State, &resp.Diagnostics)
}

func (r *keepOnceResource) kept(ctx context.Context, state tfsdk.State, diags *diag.Diagnostics) map[string]types.String {
	prior := map[string]types.String{}
	for _, attr := range r.attrs {
		var v types.String
		diags.Append(state.GetAttribute(ctx, path.Root(attr), &v)...)
		prior[attr] = v
	}
	return prior
}

func (r *keepOnceResource) restore(ctx context.Context, prior map[string]types.String, state *tfsdk.State, diags *diag.Diagnostics) {
	for attr, v := range prior {
		diags.Append(state.SetAttribute(ctx, path.Root(attr), v)...)
	}
}

func (r *keepOnceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *keepOnceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}
