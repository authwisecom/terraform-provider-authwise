package provider

import (
	"context"
	"strings"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Guard (guard-control, apis v0.22.0, #32) is a second service behind the
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
)

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

// Schema makes network_id required where a resource has one: the provider
// has no network default for it to fall back to.
func (r *guardResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
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

// guardTenantTypeName is the Terraform type the tenant wrapper substitutes
// for.
const guardTenantTypeName = "authwise_guard_tenant"

// guardTenantResource is the generated tenant with create and delete:
// guard-control registers a kit tenant (RegisterTenant, which takes only
// the id and display name, and returns the tenant already registered) and
// unregisters it (UnregisterTenant, refused while it has networks or
// users). Labels and config set at create follow as a patch.
type guardTenantResource struct {
	inner  resource.Resource
	client guardpb.GuardControlServiceClient
}

func newGuardTenantResource() resource.Resource {
	return &guardTenantResource{inner: generated.NewTenantResource()}
}

func (r *guardTenantResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *guardTenantResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

func (r *guardTenantResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.client, _ = pd.Clients[guardClientKey].(guardpb.GuardControlServiceClient)
	}
}

func (r *guardTenantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	plan := generated.NewTenantModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want, diags := plan.ToProto(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	t, err := r.client.RegisterTenant(ctx, &guardpb.RegisterTenantRequest{
		TenantId:    plan.GuardTenantId.ValueString(),
		DisplayName: plan.DisplayName.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("cannot register Guard tenant", err.Error())
		return
	}

	var mask []string
	if !plan.Labels.IsNull() && !plan.Labels.IsUnknown() {
		mask = append(mask, "labels")
	}
	if !plan.Config.IsNull() && !plan.Config.IsUnknown() {
		mask = append(mask, "config")
	}
	if len(mask) > 0 {
		if t, err = r.client.PatchTenant(ctx, &guardpb.PatchTenantRequest{
			Name:       t.GetName(),
			Tenant:     want,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: mask},
		}); err != nil {
			resp.Diagnostics.AddError("Guard tenant registered, but its labels and config were refused", err.Error())
			return
		}
	}

	state := generated.NewTenantModel()
	resp.Diagnostics.Append(state.FromProto(ctx, t)...)
	state.GuardTenantId = plan.GuardTenantId
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *guardTenantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *guardTenantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *guardTenantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	var name types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(tfruntime.NameAttribute), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UnregisterTenant(ctx, &guardpb.UnregisterTenantRequest{Name: name.ValueString()})
	if status.Code(err) == codes.NotFound {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("cannot unregister Guard tenant", err.Error())
	}
}

func (r *guardTenantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

// guardInviteTypeName is the Terraform type the invite wrapper substitutes
// for.
const guardInviteTypeName = "authwise_guard_invite"

// inviteOnce are the invite attributes guard-control returns only in the
// create response.
var inviteOnce = []string{"code", "url"}

// guardInviteResource is the generated invite with its code and url kept
// across reads: no read returns them, so a refresh would otherwise null
// them. An imported invite has neither.
type guardInviteResource struct {
	inner resource.Resource
}

func newGuardInviteResource() resource.Resource {
	return &guardInviteResource{inner: generated.NewInviteResource()}
}

func (r *guardInviteResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *guardInviteResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

func (r *guardInviteResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *guardInviteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *guardInviteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	prior := map[string]types.String{}
	for _, attr := range inviteOnce {
		var v types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(attr), &v)...)
		prior[attr] = v
	}
	if resp.Diagnostics.HasError() {
		return
	}

	r.inner.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		return
	}

	for attr, v := range prior {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(attr), v)...)
	}
}

func (r *guardInviteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *guardInviteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *guardInviteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}
