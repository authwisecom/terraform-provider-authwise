package provider

import (
	"context"
	"regexp"

	"github.com/activatedio/tfinfra/pkg/aip"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &domainActivationResource{}
	_ resource.ResourceWithConfigure   = &domainActivationResource{}
	_ resource.ResourceWithImportState = &domainActivationResource{}
	_ resource.ResourceWithMoveState   = &domainActivationResource{}
)

// domainActivationResource is the point in an apply where a custom host
// routes: PROVISIONING (certificate issued, edge entry attached) or ACTIVE.
// It sits between the challenge record (depends_on) and the traffic record
// (which depends on it). It does not wait for ACTIVE: kit routes a host from
// PROVISIONING (CUSTOM_DOMAINS §5.3, §5.5.1), and ACTIVE needs the
// controller's probe of public DNS, which needs the traffic record, so
// waiting for it would never end (#46). It owns nothing on the server:
// create waits, destroy forgets.
//
// One implementation serves authwise_domain_activation (#47) and the
// deprecated authwise_issuer_domain_activation (#45), which differ in the
// attribute naming the domain and the RPCs reading it.
type domainActivationResource struct {
	domainPoller
	activationKind
}

// activationKind is what tells the two activation resources apart.
type activationKind struct {
	// typeName follows the provider's name, as in authwise_domain_activation.
	typeName string
	// attribute names the domain's full resource name.
	attribute    string
	namePattern  *regexp.Regexp
	nameShape    string
	target       string
	clientKey    string
	deprecation  string
	configureGet func(client any) func(context.Context, string) (*domain, error)
}

var domainActivation = activationKind{
	typeName:    "domain_activation",
	attribute:   "domain",
	namePattern: domainNamePattern,
	nameShape:   "tenants/{t}/domains/{d}",
	target:      "authwise_domain",
	clientKey:   domainsClientKey,
	configureGet: func(client any) func(context.Context, string) (*domain, error) {
		if c, ok := client.(domainAPI); ok {
			return c.GetDomain
		}
		return nil
	},
}

var issuerDomainActivation = activationKind{
	typeName:    "issuer_domain_activation",
	attribute:   "issuer_domain",
	namePattern: issuerDomainNamePattern,
	nameShape:   "tenants/{t}/issuers/{i}/issuer-domains/{d}",
	target:      "authwise_issuer_domain",
	clientKey:   issuerDomainsClientKey,
	deprecation: "kit merged issuer domains into domains (kit#808): use authwise_domain_activation with domain = authwise_domain.<name>.name. " +
		"A moved block carries the state across. This resource goes once kit removes the issuer-domains API (kit#810).",
	configureGet: func(client any) func(context.Context, string) (*domain, error) {
		if c, ok := client.(issuerDomainAPI); ok {
			return c.GetIssuerDomain
		}
		return nil
	},
}

func newDomainActivationResource() resource.Resource {
	return &domainActivationResource{domainPoller{poll: domainPollInterval}, domainActivation}
}

func newIssuerDomainActivationResource() resource.Resource {
	return &domainActivationResource{domainPoller{poll: domainPollInterval}, issuerDomainActivation}
}

func (r *domainActivationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.typeName
}

func (r *domainActivationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
	}

	description := "Waits until a custom host (`" + r.target + "`) routes: `PROVISIONING` or `ACTIVE`, its certificate issued and attached to the hostname. " +
		"Put it between the domain's two DNS records, with `depends_on` naming the challenge record, and make the traffic record depend on it. One apply then creates " +
		"the domain and the challenge record, waits for the certificate, and only then points traffic at the hostname, so no request ever meets a missing certificate.\n\n" +
		"It does not wait for `ACTIVE`. Authwise confirms `ACTIVE` by finding the traffic record in public DNS, so the domain reaches it on the controller's next " +
		"check once the traffic record has propagated, after this resource is created. `state` says which of the two it saw.\n\n" +
		"It manages nothing in Authwise. Destroying it only removes it from state; a domain that leaves `ACTIVE` later (`DEGRADED`) is not replaced."
	if r.deprecation != "" {
		description = "**Deprecated:** use `authwise_domain_activation` (kit#808); a `moved` block carries the state.\n\n" + description
	}

	resp.Schema = schema.Schema{
		DeprecationMessage:  r.deprecation,
		MarkdownDescription: description,
		Attributes: map[string]schema.Attribute{
			r.attribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The domain's full resource name, `" + r.target + ".<name>.name`. Changing it, as replacing the domain does, waits again.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(r.namePattern,
					"must be a domain's name, "+r.nameShape+": use "+r.target+".<name>.name")},
			},
			hostnameAttribute:        computed("The domain's hostname."),
			"state":                  computed("The domain's state as last read: `PROVISIONING` or `ACTIVE` once created, and whatever it is now after a refresh."),
			"certificate_expires_at": computed("When the domain's current certificate expires, RFC 3339."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *domainActivationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, pd := providerClient[any](req, r.clientKey, &resp.Diagnostics)
	if pd == nil {
		return
	}
	if r.get = r.configureGet(client); r.get == nil {
		resp.Diagnostics.AddError("missing client", "provider data key "+r.clientKey+" does not serve "+r.target)
	}
}

// domainName is the configured or saved name of the domain.
func (r *domainActivationResource) domainName(ctx context.Context, from interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}, diags *diag.Diagnostics) string {
	var name types.String
	diags.Append(from.GetAttribute(ctx, path.Root(r.attribute), &name)...)
	return name.ValueString()
}

// Create waits for PROVISIONING or ACTIVE. A failed wait saves nothing, so
// the next apply waits again rather than carrying on as if the domain were
// serving.
func (r *domainActivationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	name := r.domainName(ctx, req.Plan, &resp.Diagnostics)
	var to timeouts.Value
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &to)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, d := to.Create(ctx, domainDefaultTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := r.get(ctx, name)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root(r.attribute), "read domain failed", err.Error())
		return
	}
	// A native host has no lifecycle: it routes as soon as it exists, and
	// waiting on it would never end. Only authwise_domain can name one.
	if r.attribute == domainActivation.attribute && !out.custom() {
		resp.Diagnostics.AddAttributeError(path.Root(r.attribute), "not a custom host",
			name+" has no issuer_id: a native host routes as soon as it exists, and there is nothing to wait for. Activate a custom host (authwise_domain with issuer_id).")
		return
	}
	if out = r.waitFor(ctx, out, routes, "state PROVISIONING or ACTIVE", &resp.Diagnostics); resp.Diagnostics.HasError() {
		return
	}

	resp.State.Raw = req.Plan.Raw
	r.setDomain(ctx, &resp.State, out, &resp.Diagnostics)
}

// Read follows the domain: gone or DELETING removes the activation too, so
// a recreated domain is waited for again.
func (r *domainActivationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	name := r.domainName(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.get(ctx, name)
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read domain failed", err.Error())
		return
	}
	if out.State == domainDeleting {
		tflog.Warn(ctx, "domain is being deleted; removing its activation from state", map[string]any{r.attribute: out.Name})
		resp.State.RemoveResource(ctx)
		return
	}

	r.setDomain(ctx, &resp.State, out, &resp.Diagnostics)
}

// Update changes only timeouts, which mean nothing once the wait is over.
func (r *domainActivationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	var to timeouts.Value
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &to)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.Raw = req.State.Raw
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("timeouts"), to)...)
}

// Delete forgets: the domain is the domain resource's to delete.
func (r *domainActivationResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

// ImportState takes the domain's name. Importing does not wait; the refresh
// that follows reads the domain's state as it is.
func (r *domainActivationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if !r.namePattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("invalid import ID for "+r.typeName,
			"expected the domain's name, "+r.nameShape+"; got "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.attribute), req.ID)...)
}

func (r *domainActivationResource) setDomain(ctx context.Context, state *tfsdk.State, d *domain, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root(hostnameAttribute), d.Hostname)...)
	diags.Append(state.SetAttribute(ctx, path.Root("state"), d.State)...)
	diags.Append(state.SetAttribute(ctx, path.Root("certificate_expires_at"), timestampValue(d.CertificateExpiresAt))...)
}

// MoveState takes authwise_issuer_domain_activation's state into
// authwise_domain_activation in a moved block: issuer_domain becomes
// domain, the issuer domain's name the domain's.
func (r *domainActivationResource) MoveState(ctx context.Context) []resource.StateMover {

	if r.attribute != domainActivation.attribute {
		return nil
	}

	source := &resource.SchemaResponse{}
	(&domainActivationResource{activationKind: issuerDomainActivation}).Schema(ctx, resource.SchemaRequest{}, source)

	return []resource.StateMover{{
		SourceSchema: &source.Schema,
		StateMover: func(ctx context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {

			if req.SourceTypeName != "authwise_issuer_domain_activation" {
				return
			}

			var prior struct {
				IssuerDomain         types.String   `tfsdk:"issuer_domain"`
				Hostname             types.String   `tfsdk:"hostname"`
				State                types.String   `tfsdk:"state"`
				CertificateExpiresAt types.String   `tfsdk:"certificate_expires_at"`
				Timeouts             timeouts.Value `tfsdk:"timeouts"`
			}
			resp.Diagnostics.Append(req.SourceState.Get(ctx, &prior)...)
			if resp.Diagnostics.HasError() {
				return
			}

			name, err := domainMovedName(prior.IssuerDomain.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("unexpected issuer domain name", err.Error())
				return
			}

			resp.Diagnostics.Append(resp.TargetState.Set(ctx, struct {
				Domain               types.String   `tfsdk:"domain"`
				Hostname             types.String   `tfsdk:"hostname"`
				State                types.String   `tfsdk:"state"`
				CertificateExpiresAt types.String   `tfsdk:"certificate_expires_at"`
				Timeouts             timeouts.Value `tfsdk:"timeouts"`
			}{types.StringValue(name), prior.Hostname, prior.State, prior.CertificateExpiresAt, prior.Timeouts})...)
		},
	}}
}
