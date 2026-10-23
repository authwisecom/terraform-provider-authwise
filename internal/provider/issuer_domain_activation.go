package provider

import (
	"context"
	"time"

	"github.com/activatedio/tfinfra/pkg/aip"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	issuerDomainActivationTypeName = "issuer_domain_activation"
	issuerDomainAttribute          = "issuer_domain"
)

var (
	_ resource.Resource                = &issuerDomainActivationResource{}
	_ resource.ResourceWithConfigure   = &issuerDomainActivationResource{}
	_ resource.ResourceWithImportState = &issuerDomainActivationResource{}
)

// issuerDomainActivationResource is authwise_issuer_domain_activation: the
// point in an apply where an issuer domain is ACTIVE. It sits between the
// challenge record (depends_on) and the traffic record (which depends on it),
// so one apply follows "challenge first, traffic after Active". It owns
// nothing on the server: create waits, destroy forgets.
type issuerDomainActivationResource struct {
	issuerDomainPoller
}

type issuerDomainActivationModel struct {
	IssuerDomain         types.String   `tfsdk:"issuer_domain"`
	Hostname             types.String   `tfsdk:"hostname"`
	State                types.String   `tfsdk:"state"`
	CertificateExpiresAt types.String   `tfsdk:"certificate_expires_at"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

func newIssuerDomainActivationResource() resource.Resource {
	return &issuerDomainActivationResource{issuerDomainPoller{poll: issuerDomainPollInterval}}
}

func (r *issuerDomainActivationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + issuerDomainActivationTypeName
}

func (r *issuerDomainActivationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Waits until an `authwise_issuer_domain` is `ACTIVE`: its certificate is issued and attached. Put it between the domain's two DNS records, " +
			"with `depends_on` naming the challenge record, and make the traffic record depend on it. One apply then creates the domain and the challenge record, " +
			"waits for the certificate, and only then points traffic at the hostname, so no request ever meets a missing certificate.\n\n" +
			"It manages nothing in Authwise. Destroying it only removes it from state; a domain that leaves `ACTIVE` later (`DEGRADED`) is not replaced.",
		Attributes: map[string]schema.Attribute{
			issuerDomainAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The domain's full resource name, `authwise_issuer_domain.<name>.name`. Changing it, as replacing the domain does, waits again.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(issuerDomainNamePattern,
					"must be an issuer domain's name, tenants/{t}/issuers/{i}/issuer-domains/{d}: use authwise_issuer_domain.<name>.name")},
			},
			"hostname":               computed("The domain's hostname."),
			"state":                  computed("The domain's state as last read: `ACTIVE` once created, and whatever it is now after a refresh."),
			"certificate_expires_at": computed("When the domain's current certificate expires, RFC 3339."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *issuerDomainActivationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(req, &resp.Diagnostics)
}

// Create waits for ACTIVE. A failed wait saves nothing, so the next apply
// waits again rather than carrying on as if the domain were serving.
func (r *issuerDomainActivationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan issuerDomainActivationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, d := plan.Timeouts.Create(ctx, issuerDomainDefaultTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := r.client.GetIssuerDomain(ctx, plan.IssuerDomain.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root(issuerDomainAttribute), "read issuer domain failed", err.Error())
		return
	}
	if out = r.waitFor(ctx, out, isActive, "state ACTIVE", &resp.Diagnostics); resp.Diagnostics.HasError() {
		return
	}

	plan.fromDomain(out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read follows the domain: gone or DELETING removes the activation too, so
// a recreated domain is waited for again.
func (r *issuerDomainActivationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state issuerDomainActivationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetIssuerDomain(ctx, state.IssuerDomain.ValueString())
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read issuer domain failed", err.Error())
		return
	}
	if out.State == issuerDomainDeleting {
		tflog.Warn(ctx, "issuer domain is being deleted; removing its activation from state", map[string]any{issuerDomainAttribute: out.Name})
		resp.State.RemoveResource(ctx)
		return
	}

	state.fromDomain(out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update changes only timeouts, which mean nothing once the wait is over.
func (r *issuerDomainActivationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	var plan, state issuerDomainActivationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Hostname, plan.State, plan.CertificateExpiresAt = state.Hostname, state.State, state.CertificateExpiresAt
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete forgets: the domain is authwise_issuer_domain's to delete.
func (r *issuerDomainActivationResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

// ImportState takes the domain's name. Importing does not wait; the refresh
// that follows reads the domain's state as it is.
func (r *issuerDomainActivationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if !issuerDomainNamePattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("invalid import ID for issuer domain activation",
			"expected the issuer domain's name, tenants/{t}/issuers/{i}/issuer-domains/{d}; got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(issuerDomainAttribute), req.ID)...)
}

func (m *issuerDomainActivationModel) fromDomain(d *issuerDomain) {
	m.Hostname = types.StringValue(d.Hostname)
	m.State = types.StringValue(d.State)
	m.CertificateExpiresAt = types.StringValue("")
	if d.CertificateExpiresAt != nil {
		m.CertificateExpiresAt = types.StringValue(d.CertificateExpiresAt.UTC().Format(time.RFC3339))
	}
}
