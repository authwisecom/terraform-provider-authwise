package provider

import (
	"context"
	"regexp"

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

// preferredDomainPath is the update_mask path the resource owns on the
// issuer (kit#857).
const preferredDomainPath = "preferred_domain_id"

// issuerNamePattern is an issuer's full resource name.
var issuerNamePattern = regexp.MustCompile(`^tenants/[^/]+/issuers/[^/]+$`)

var (
	_ resource.Resource                = &issuerPreferredDomainResource{}
	_ resource.ResourceWithConfigure   = &issuerPreferredDomainResource{}
	_ resource.ResourceWithImportState = &issuerPreferredDomainResource{}
)

// issuerPreferredDomainResource manages Issuer.preferred_domain_id apart
// from authwise_issuer (#52). A custom domain names its issuer, so the
// issuer naming the custom domain back would be a cycle; this resource
// sits after both. authwise_issuer reads the field back, output only.
//
// Destroy sets the issuer back to its native host. kit clears the field
// itself when the domain is released (ON DELETE SET NULL), which Read sees
// as the resource gone.
type issuerPreferredDomainResource struct {
	client identitypb.AuthwiseIdentityServiceClient
}

type issuerPreferredDomainModel struct {
	Issuer   types.String `tfsdk:"issuer"`
	DomainID types.String `tfsdk:"domain_id"`
}

func newIssuerPreferredDomainResource() resource.Resource {
	return &issuerPreferredDomainResource{}
}

func (r *issuerPreferredDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_issuer_preferred_domain"
}

func (r *issuerPreferredDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The host an issuer's mailed links use: the invitation accept, the password reset, the address verification (`Issuer.preferred_domain_id`). " +
			"Without it they use the issuer's native host. Point it at the custom domain your applications sign in on, so an invitation's one-button accept " +
			"leaves the person signed in where the application looks.\n\n" +
			"It is a resource of its own because a custom domain names its issuer: the issuer naming the domain back would be a cycle. " +
			"`authwise_issuer.preferred_domain_id` reads the value back. Destroying this resource sets the issuer back to its native host, " +
			"and so does releasing the domain, on kit's side; the next refresh then finds this resource gone.",
		Attributes: map[string]schema.Attribute{
			"issuer": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The issuer's full resource name, `authwise_issuer.<name>.name`. Changing it sets the old issuer back to its native host.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(issuerNamePattern,
					"must be an issuer's name, tenants/{t}/issuers/{i}: use authwise_issuer.<name>.name")},
			},
			"domain_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "One of the issuer's own domains: a custom domain attached to it (`authwise_domain.<name>.domain_id` with `issuer_id` set), " +
					"or its native domain. kit refuses any other. Until the custom domain is `ACTIVE`, kit keeps building links on the native host.",
				Validators: []validator.String{tfruntime.ReferenceID("d", "domain", "authwise_domain.<name>.domain_id"), stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *issuerPreferredDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *issuerPreferredDomainResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("issuer_preferred_domain resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *issuerPreferredDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *issuerPreferredDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *issuerPreferredDomainResource) write(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diags *diag.Diagnostics) {

	if !r.configured(diags) {
		return
	}

	var m issuerPreferredDomainModel
	diags.Append(plan.Get(ctx, &m)...)
	if diags.HasError() {
		return
	}

	if err := r.patch(ctx, m.Issuer.ValueString(), m.DomainID.ValueString()); err != nil {
		diags.AddError("setting the issuer's preferred domain failed", err.Error())
		return
	}

	diags.Append(state.Set(ctx, m)...)
}

// Read follows the issuer: gone, or back on its native host (the domain
// released, or the field reset outside Terraform), removes the resource,
// so the next plan sets it again.
func (r *issuerPreferredDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var m issuerPreferredDomainModel
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
	if i.GetPreferredDomainId() == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	m.DomainID = types.StringValue(i.GetPreferredDomainId())
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// Delete sets the issuer back to its native host. An issuer already gone
// has nothing to reset.
func (r *issuerPreferredDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var m issuerPreferredDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.patch(ctx, m.Issuer.ValueString(), ""); err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("resetting the issuer's preferred domain failed", err.Error())
	}
}

func (r *issuerPreferredDomainResource) patch(ctx context.Context, issuer, domainID string) error {
	_, err := r.client.PatchIssuer(ctx, &identitypb.PatchIssuerRequest{
		Name:       issuer,
		Issuer:     &corepb.Issuer{PreferredDomainId: domainID},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{preferredDomainPath}},
	})
	return err
}

// ImportState takes the issuer's name; the refresh that follows reads the
// domain.
func (r *issuerPreferredDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if !issuerNamePattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("invalid import ID for issuer_preferred_domain",
			"expected the issuer's name, tenants/{t}/issuers/{i}; got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("issuer"), req.ID)...)
}
