package provider

import (
	"context"
	"regexp"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	issuerDomainTypeName   = "issuer_domain"
	issuerDomainCollection = "issuer-domains"

	// issuerDomainsClientKey is the provider data key of the issuerDomainAPI.
	issuerDomainsClientKey = "issuerDomains"
)

// issuerDomainDeprecation is the deprecated aliases' message (#47).
const issuerDomainDeprecation = "kit merged issuer domains into domains (kit#808): use authwise_domain with issuer_id, and authwise_domain_activation. " +
	"A moved block carries the state across without touching the domain. This resource goes once kit removes the issuer-domains API (kit#810)."

var issuerDomainScope = aip.NewScope("tenants", "issuers")

// issuerDomainNamePattern is an issuer domain's full resource name.
var issuerDomainNamePattern = regexp.MustCompile(`^tenants/[^/]+/issuers/[^/]+/issuer-domains/[^/]+$`)

// issuerDomainAPI is the slice of the identity service the resource calls.
// Errors are gRPC statuses, as the identity client's are.
type issuerDomainAPI interface {
	CreateIssuerDomain(ctx context.Context, parent, hostname string) (*domain, error)
	GetIssuerDomain(ctx context.Context, name string) (*domain, error)
	DeleteIssuerDomain(ctx context.Context, name string) error
}

var (
	_ resource.Resource                = &issuerDomainResource{}
	_ resource.ResourceWithConfigure   = &issuerDomainResource{}
	_ resource.ResourceWithImportState = &issuerDomainResource{}
)

// issuerDomainResource is authwise_issuer_domain (#44), deprecated (#47):
// authwise_domain with issuer_id is the same row since kit#808, and kit
// keeps the issuer-domains RPCs this calls for one release as aliases.
type issuerDomainResource struct {
	domainPoller
	client   issuerDomainAPI
	defaults map[string]string
}

type issuerDomainModel struct {
	Name                 types.String   `tfsdk:"name"`
	IssuerDomainID       types.String   `tfsdk:"issuer_domain_id"`
	TenantID             types.String   `tfsdk:"tenant_id"`
	IssuerID             types.String   `tfsdk:"issuer_id"`
	Hostname             types.String   `tfsdk:"hostname"`
	DNSRecords           types.List     `tfsdk:"dns_records"`
	TrafficTarget        types.String   `tfsdk:"traffic_target"`
	State                types.String   `tfsdk:"state"`
	FailureCode          types.String   `tfsdk:"failure_code"`
	FailureMessage       types.String   `tfsdk:"failure_message"`
	FailureRecord        types.String   `tfsdk:"failure_record"`
	CertificateExpiresAt types.String   `tfsdk:"certificate_expires_at"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

func newIssuerDomainResource() resource.Resource {
	return &issuerDomainResource{domainPoller: domainPoller{poll: domainPollInterval}}
}

func (r *issuerDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + issuerDomainTypeName
}

func (r *issuerDomainResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}

	parent := func(desc string, v validator.String) schema.StringAttribute {
		return schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: desc + "; overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.",
			PlanModifiers:       []planmodifier.String{tfruntime.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
			Validators:          []validator.String{v},
		}
	}

	resp.Schema = schema.Schema{
		DeprecationMessage: issuerDomainDeprecation,
		MarkdownDescription: "**Deprecated:** use `authwise_domain` with `issuer_id` (kit#808); a `moved` block carries the state.\n\n" +
			"A hostname of yours that serves an issuer, such as `login.acme.com`. Authwise obtains and renews its certificate; you add two DNS records, " +
			"read from `dns_records`. The issuer keeps answering at its own host as well, and each host is its own issuer URL (`iss`).\n\n" +
			"Create returns once the records are known, so DNS resources can take them in the same apply. Add the challenge record first and point traffic only once `state` is `PROVISIONING` or `ACTIVE`, " +
			"so the certificate exists before any request arrives: `authwise_issuer_domain_activation` waits for that between the two records, in one apply. Both records must be DNS-only (on Cloudflare, `proxied = false`), and the challenge record stays for as long as the domain does: renewal depends on it.\n\n" +
			"Destroying the domain waits until Authwise has released the hostname, which takes the cloud side's teardown.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full resource name (`tenants/{t}/issuers/{i}/issuer-domains/{d}`); serves as the Terraform ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"issuer_domain_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned resource id — the last segment of `name`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			tenantIDAttribute: parent("Parent identifier `tenant_id`", tfruntime.ReferenceID("t", "tenant", "")),
			issuerIDAttribute: parent("The issuer the hostname serves (`i-…`)",
				tfruntime.ReferenceID("i", "issuer", "authwise_issuer.<name>.issuer_id")),
			"hostname": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The fully qualified hostname, lower case, without a trailing dot. It must be a subdomain: a CNAME cannot sit at a zone apex. " +
					"Unique across Authwise; kit refuses one already claimed. Changing it replaces the domain.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(hostnamePattern,
					"must be a lower-case fully qualified hostname such as login.acme.com, without a scheme, path or trailing dot")},
			},
			"dns_records": schema.ListNestedAttribute{
				Computed: true,
				MarkdownDescription: "The records to add at your DNS provider, one per `purpose`: `traffic` (the hostname, a CNAME to the issuer's own host) and `challenge` (the certificate authority's proof of control). " +
					"Known after create. Key DNS resources on `purpose`, not on these values, so the keys are known at plan; see the example.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						tfruntime.NameAttribute: computed("The record's name, a fully qualified hostname."),
						recordTypeAttribute:     computed("The record type, `CNAME`."),
						recordValueAttribute:    computed("The record's content."),
						recordPurposeAttribute:  computed("`traffic` or `challenge`."),
					},
				},
			},
			"traffic_target": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The issuer's own Authwise host, such as `example.id.authwise.com`: the value of the `traffic` record.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"state": computed("`PENDING_DNS` (waiting for the challenge record), `VERIFYING` (issuing the certificate), `PROVISIONING` (attaching it; the hostname routes), `ACTIVE`, " +
				"`DEGRADED` (serving, but the challenge record is gone and renewal will fail), `FAILED` (see `failure_code`) or `DELETING`."),
			"failure_code": computed("Why the domain is `FAILED` or `DEGRADED`; empty otherwise. One of `DNS_NOT_FOUND` (a record is missing), `CAA` (a CAA record excludes the certificate authority), " +
				"`CONFIG` (a record exists with the wrong content, or is proxied), `RATE_LIMITED` (the certificate authority's limit; it retries), `PROBE_FAILED` (the hostname did not answer as the issuer) or `PROVIDER_ERROR` (Authwise's side)."),
			"failure_message":        computed("`failure_code` in words, with the record or setting to fix."),
			"failure_record":         computed("The `purpose` of the record the failure is about, `traffic` or `challenge`; empty when it is about neither."),
			"certificate_expires_at": computed("When the current certificate expires, RFC 3339; empty before the first is issued."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *issuerDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, pd := providerClient[issuerDomainAPI](req, issuerDomainsClientKey, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client, r.get, r.defaults = client, client.GetIssuerDomain, pd.Defaults
}

func (r *issuerDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan issuerDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, d := plan.Timeouts.Create(ctx, domainDefaultTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ids := map[string]string{}
	for attr, v := range map[string]types.String{tenantIDAttribute: plan.TenantID, issuerIDAttribute: plan.IssuerID} {
		ids[attr] = r.defaults[attr]
		if s := v.ValueString(); s != "" {
			ids[attr] = s
		}
	}
	parent, err := issuerDomainScope.ComposeParent(ids)
	if err != nil {
		resp.Diagnostics.AddError("cannot resolve parent for issuer domain", err.Error()+"; set it on the resource or as a provider default")
		return
	}

	out, err := r.client.CreateIssuerDomain(ctx, parent, plan.Hostname.ValueString())
	if err != nil {
		summary, detail := domainCreateError(err)
		resp.Diagnostics.AddAttributeError(path.Root("hostname"), summary, detail)
		return
	}

	// The row exists from here: state is saved even when a wait below
	// fails, so Terraform taints the domain rather than losing it.
	defer func() {
		resp.Diagnostics.Append(plan.fromDomain(out)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}()

	recordsCtx, cancelRecords := context.WithTimeout(ctx, domainRecordsWait)
	defer cancelRecords()
	out = r.waitFor(recordsCtx, out, hasRecords, "its DNS records", &resp.Diagnostics)
}

func (r *issuerDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state issuerDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetIssuerDomain(ctx, state.Name.ValueString())
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read issuer domain failed", err.Error())
		return
	}

	// A domain someone else deleted is gone for Terraform's purposes: it
	// no longer routes, and kit removes the row once the cloud side is.
	if out.State == domainDeleting {
		tflog.Warn(ctx, "issuer domain is being deleted outside Terraform; removing it from state", map[string]any{tfruntime.NameAttribute: out.Name})
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(state.fromDomain(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update has nothing to send: hostname and the parents replace the domain,
// and only timeouts is left to change. It reads the domain back.
func (r *issuerDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state issuerDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, d := plan.Timeouts.Update(ctx, domainDefaultTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := r.client.GetIssuerDomain(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("read issuer domain failed", err.Error())
		return
	}

	resp.Diagnostics.Append(plan.fromDomain(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is two-phase on the server: the row goes DELETING and kit removes
// it once the controller reports the cloud side gone. Waiting for that means
// a replacement, or another tenant, can claim the hostname when this returns.
func (r *issuerDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state issuerDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, d := state.Timeouts.Delete(ctx, domainDefaultTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := state.Name.ValueString()

	if err := r.client.DeleteIssuerDomain(ctx, name); err != nil {
		if aip.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("delete issuer domain failed", err.Error())
		return
	}

	r.waitGone(ctx, name, timeout, &resp.Diagnostics)
}

// ImportState takes the full resource name; the parents read back from it.
func (r *issuerDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := issuerDomainScope.ParseName(issuerDomainCollection, req.ID); err != nil {
		resp.Diagnostics.AddError("invalid import ID for issuer domain", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), req.ID)...)
}

// issuerDomainClient is issuerDomainAPI over kit's identity service.
type issuerDomainClient struct {
	client identitypb.AuthwiseIdentityServiceClient
}

func (c issuerDomainClient) CreateIssuerDomain(ctx context.Context, parent, hostname string) (*domain, error) {
	out, err := c.client.CreateIssuerDomain(ctx, &identitypb.CreateIssuerDomainRequest{
		Parent:       parent,
		IssuerDomain: &corepb.IssuerDomain{Hostname: hostname},
	})
	if err != nil {
		return nil, err
	}
	return issuerDomainFromProto(out), nil
}

func (c issuerDomainClient) GetIssuerDomain(ctx context.Context, name string) (*domain, error) {
	out, err := c.client.GetIssuerDomain(ctx, &identitypb.GetIssuerDomainRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return issuerDomainFromProto(out), nil
}

func (c issuerDomainClient) DeleteIssuerDomain(ctx context.Context, name string) error {
	_, err := c.client.DeleteIssuerDomain(ctx, &identitypb.DeleteIssuerDomainRequest{Name: name})
	return err
}

func issuerDomainFromProto(e *corepb.IssuerDomain) *domain {

	d := &domain{
		Name:          e.GetName(),
		Hostname:      e.GetHostname(),
		State:         enumName(e.GetState().String(), "ISSUER_DOMAIN_STATE_"),
		TrafficTarget: e.GetTrafficTarget(),
	}
	if f := e.GetFailure(); f != nil {
		d.FailureCode = enumName(f.GetCode().String(), "ISSUER_DOMAIN_FAILURE_CODE_")
		d.FailureMessage = f.GetMessage()
		d.FailureRecord = f.GetRecord()
	}
	if ts := e.GetCertificateExpiresAt(); ts != nil {
		t := ts.AsTime()
		d.CertificateExpiresAt = &t
	}
	for _, r := range e.GetDnsRecords() {
		d.DNSRecords = append(d.DNSRecords, domainRecord{Name: r.GetName(), Type: r.GetType(), Value: r.GetValue(), Purpose: r.GetPurpose()})
	}

	return d
}

func (m *issuerDomainModel) fromDomain(d *domain) diag.Diagnostics {

	var diags diag.Diagnostics

	ids, id, err := issuerDomainScope.ParseName(issuerDomainCollection, d.Name)
	if err != nil {
		diags.AddError("unexpected issuer domain name", err.Error())
		return diags
	}

	m.Name = types.StringValue(d.Name)
	m.IssuerDomainID = types.StringValue(id)
	m.TenantID = types.StringValue(ids[tenantIDAttribute])
	m.IssuerID = types.StringValue(ids[issuerIDAttribute])
	m.Hostname = types.StringValue(d.Hostname)
	m.State = types.StringValue(d.State)
	m.TrafficTarget = types.StringValue(d.TrafficTarget)
	m.FailureCode = types.StringValue(d.FailureCode)
	m.FailureMessage = types.StringValue(d.FailureMessage)
	m.FailureRecord = types.StringValue(d.FailureRecord)
	m.CertificateExpiresAt = timestampValue(d.CertificateExpiresAt)

	list, dd := domainRecordsValue(d.DNSRecords)
	diags.Append(dd...)
	m.DNSRecords = list

	return diags
}
