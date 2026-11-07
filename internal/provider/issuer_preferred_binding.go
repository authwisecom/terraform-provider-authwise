package provider

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// preferredBindingPath is the update_mask path the resource owns on the
// issuer (kit#911, D4).
const preferredBindingPath = "preferred_binding_id"

var (
	_ resource.Resource                = &issuerPreferredBindingResource{}
	_ resource.ResourceWithConfigure   = &issuerPreferredBindingResource{}
	_ resource.ResourceWithImportState = &issuerPreferredBindingResource{}
)

// issuerPreferredBindingResource manages Issuer.preferred_binding_id
// (kit#911) apart from authwise_issuer, as authwise_issuer_preferred_domain
// did preferred_domain_id (#52): a binding names its issuer, so the issuer
// naming the binding back would be a cycle; this resource sits after both.
//
// Destroy sets the issuer back to its first binding (an empty value). A
// deleted binding takes the field with it on kit's side, which Read sees as
// the resource gone.
type issuerPreferredBindingResource struct {
	client identitypb.AuthwiseIdentityServiceClient
}

type issuerPreferredBindingModel struct {
	Issuer    types.String `tfsdk:"issuer"`
	BindingID types.String `tfsdk:"binding_id"`
}

func newIssuerPreferredBindingResource() resource.Resource {
	return &issuerPreferredBindingResource{}
}

func (r *issuerPreferredBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_issuer_preferred_binding"
}

func (r *issuerPreferredBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The binding an issuer's mailed links use: the invitation accept, the password reset, the address verification (`Issuer.preferred_binding_id`, kit#911). " +
			"Without it they use the issuer's first binding. Point it at the binding on the custom domain your applications sign in on, so an invitation's one-button accept " +
			"leaves the person signed in where the application looks. A binding on a custom domain that is not `ACTIVE` when a link is built falls back to the first routable one.\n\n" +
			"It is a resource of its own because a binding names its issuer: the issuer naming the binding back would be a cycle. " +
			"`authwise_issuer.preferred_binding_id` reads the value back. Destroying this resource sets the issuer back to its first binding, " +
			"and so does deleting the binding, on kit's side; the next refresh then finds this resource gone.",
		Attributes: map[string]schema.Attribute{
			"issuer": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The issuer's full resource name, `authwise_issuer.<name>.name`. Changing it sets the old issuer back to its first binding.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(issuerNamePattern,
					"must be an issuer's name, tenants/{t}/issuers/{i}: use authwise_issuer.<name>.name")},
			},
			"binding_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "One of the issuer's bindings (`idb-…`), `authwise_issuer_domain_binding.<name>.issuer_domain_binding_id`. kit refuses another issuer's.",
				Validators:          []validator.String{tfruntime.ReferenceID("idb", "issuer_domain_binding", "authwise_issuer_domain_binding.<name>.issuer_domain_binding_id"), stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *issuerPreferredBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *issuerPreferredBindingResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("issuer_preferred_binding resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *issuerPreferredBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *issuerPreferredBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *issuerPreferredBindingResource) write(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diags *diag.Diagnostics) {

	if !r.configured(diags) {
		return
	}

	var m issuerPreferredBindingModel
	diags.Append(plan.Get(ctx, &m)...)
	if diags.HasError() {
		return
	}

	if err := r.patch(ctx, m.Issuer.ValueString(), m.BindingID.ValueString()); err != nil {
		diags.AddError("setting the issuer's preferred binding failed", err.Error())
		return
	}

	diags.Append(state.Set(ctx, m)...)
}

// Read follows the issuer: gone, or back on its first binding (the binding
// deleted, or the field reset outside Terraform), removes the resource,
// so the next plan sets it again.
func (r *issuerPreferredBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var m issuerPreferredBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	i, err := r.client.GetIssuer(ctx, &identitypb.GetIssuerRequest{Name: m.Issuer.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the issuer failed", err.Error())
		return
	}
	if i.GetPreferredBindingId() == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	m.BindingID = types.StringValue(i.GetPreferredBindingId())
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// Delete sets the issuer back to its first binding. An issuer already gone
// has nothing to reset.
func (r *issuerPreferredBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var m issuerPreferredBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.patch(ctx, m.Issuer.ValueString(), ""); err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("resetting the issuer's preferred binding failed", err.Error())
	}
}

func (r *issuerPreferredBindingResource) patch(ctx context.Context, issuer, bindingID string) error {
	_, err := r.client.PatchIssuer(ctx, &identitypb.PatchIssuerRequest{
		Name:       issuer,
		Issuer:     &corepb.Issuer{PreferredBindingId: bindingID},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{preferredBindingPath}},
	})
	return err
}

// ImportState takes the issuer's name; the refresh that follows reads the
// binding.
func (r *issuerPreferredBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if !issuerNamePattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("invalid import ID for issuer_preferred_binding",
			"expected the issuer's name, tenants/{t}/issuers/{i}; got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("issuer"), req.ID)...)
}
