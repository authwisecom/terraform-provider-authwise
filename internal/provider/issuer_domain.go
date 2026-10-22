package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

const (
	issuerDomainTypeName   = "issuer_domain"
	issuerDomainCollection = "issuer-domains"

	// issuerDomainsClientKey is the provider data key of the issuerDomainAPI.
	issuerDomainsClientKey = "issuerDomains"

	issuerIDAttribute = "issuer_id"

	// A DNS record's attributes.
	recordTypeAttribute    = "type"
	recordValueAttribute   = "value"
	recordPurposeAttribute = "purpose"

	// authwiseErrorDomain is the ErrorInfo domain of kit's refusals.
	authwiseErrorDomain = "authwise.io"
)

// The states kit reports for an issuer domain (CUSTOM_DOMAINS §4.1), without
// the enum prefix.
const (
	issuerDomainPendingDNS   = "PENDING_DNS"
	issuerDomainVerifying    = "VERIFYING"
	issuerDomainProvisioning = "PROVISIONING"
	issuerDomainActive       = "ACTIVE"
	issuerDomainDegraded     = "DEGRADED"
	issuerDomainFailed       = "FAILED"
	issuerDomainDeleting     = "DELETING"
)

const (
	// issuerDomainRecordsWait bounds the wait for the DNS records: one
	// controller round trip, typically under thirty seconds.
	issuerDomainRecordsWait = 5 * time.Minute
	// issuerDomainDefaultTimeout is the create, update and delete timeout
	// when the configuration sets none. Certificate issuance and the cloud
	// side's teardown are both minutes, not seconds.
	issuerDomainDefaultTimeout = 30 * time.Minute
	issuerDomainPollInterval   = 5 * time.Second
)

var issuerDomainScope = aip.NewScope("tenants", "issuers")

// hostnamePattern is a lower-case FQDN of two or more labels, without a
// trailing dot. kit is the authority (it refuses an apex, a claimed name and
// anything below the plan gate); this only catches a URL or a stray capital
// in plan.
var hostnamePattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// issuerDomain is kit's IssuerDomain as this resource uses it: the enums as
// their names without the prefix. issuerDomainClient is the only code that
// sees the proto, so the waits are tested against a plain fake.
type issuerDomain struct {
	Name           string
	Hostname       string
	State          string
	DNSRecords     []issuerDomainRecord
	FailureCode    string
	FailureMessage string
	// FailureRecord is the record purpose the failure is about, or "".
	FailureRecord        string
	CertificateExpiresAt *time.Time
}

type issuerDomainRecord struct {
	Name, Type, Value, Purpose string
}

// issuerDomainAPI is the slice of the identity service the resource calls.
// Errors are gRPC statuses, as the identity client's are.
type issuerDomainAPI interface {
	CreateIssuerDomain(ctx context.Context, parent, hostname string) (*issuerDomain, error)
	GetIssuerDomain(ctx context.Context, name string) (*issuerDomain, error)
	DeleteIssuerDomain(ctx context.Context, name string) error
}

var (
	_ resource.Resource                = &issuerDomainResource{}
	_ resource.ResourceWithConfigure   = &issuerDomainResource{}
	_ resource.ResourceWithImportState = &issuerDomainResource{}
)

// issuerDomainResource is authwise_issuer_domain (#44): a hostname of the
// customer's that serves an issuer, with the certificate obtained and renewed
// on Authwise's edge. Hand-written because Create and Delete wait on the
// domain controller: kit returns the row at once, and the DNS records, the
// certificate and the release of a deleted hostname arrive later.
type issuerDomainResource struct {
	client   issuerDomainAPI
	defaults map[string]string
	// poll is the interval between reads while waiting.
	poll time.Duration
}

type issuerDomainModel struct {
	Name                 types.String   `tfsdk:"name"`
	IssuerDomainID       types.String   `tfsdk:"issuer_domain_id"`
	TenantID             types.String   `tfsdk:"tenant_id"`
	IssuerID             types.String   `tfsdk:"issuer_id"`
	Hostname             types.String   `tfsdk:"hostname"`
	WaitForActive        types.Bool     `tfsdk:"wait_for_active"`
	DNSRecords           types.List     `tfsdk:"dns_records"`
	State                types.String   `tfsdk:"state"`
	FailureCode          types.String   `tfsdk:"failure_code"`
	FailureMessage       types.String   `tfsdk:"failure_message"`
	FailureRecord        types.String   `tfsdk:"failure_record"`
	CertificateExpiresAt types.String   `tfsdk:"certificate_expires_at"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

func issuerDomainRecordAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		tfruntime.NameAttribute: types.StringType,
		recordTypeAttribute:     types.StringType,
		recordValueAttribute:    types.StringType,
		recordPurposeAttribute:  types.StringType,
	}
}

func newIssuerDomainResource() resource.Resource {
	return &issuerDomainResource{poll: issuerDomainPollInterval}
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
		MarkdownDescription: "A hostname of yours that serves an issuer, such as `login.acme.com`. Authwise obtains and renews its certificate; you add two DNS records, " +
			"read from `dns_records`. The issuer keeps answering at its own host as well, and each host is its own issuer URL (`iss`).\n\n" +
			"Create returns once the records are known, so DNS resources can take them in the same apply. Add the challenge record first and point traffic only once `state` is `ACTIVE`; " +
			"the certificate then exists before any request arrives. Both records must be DNS-only (on Cloudflare, `proxied = false`), and the challenge record stays for as long as the domain does: renewal depends on it.\n\n" +
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
			"wait_for_active": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "When `true`, create (and an update that sets it) waits until `state` is `ACTIVE`, up to the `create` (or `update`) timeout, default 30 minutes. " +
					"The certificate is issued only after the challenge record resolves, so set it only where that record already exists or is made outside this apply's dependency on the domain. Default `false`.",
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

	if req.ProviderData == nil {
		return
	}

	pd, ok := req.ProviderData.(*tfruntime.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("unexpected provider data", fmt.Sprintf("expected *tf.ProviderData, got %T", req.ProviderData))
		return
	}

	client, ok := pd.Clients[issuerDomainsClientKey].(issuerDomainAPI)
	if !ok {
		resp.Diagnostics.AddError("missing client", `provider data key "issuerDomains" is not an issuerDomainAPI`)
		return
	}

	r.client = client
	r.defaults = pd.Defaults
}

func (r *issuerDomainResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("authwise_issuer_domain is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
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

	timeout, d := plan.Timeouts.Create(ctx, issuerDomainDefaultTimeout)
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
		summary, detail := issuerDomainCreateError(err)
		resp.Diagnostics.AddAttributeError(path.Root("hostname"), summary, detail)
		return
	}

	// The row exists from here: state is saved even when a wait below
	// fails, so Terraform taints the domain rather than losing it.
	defer func() {
		resp.Diagnostics.Append(plan.fromDomain(out)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}()

	recordsCtx, cancelRecords := context.WithTimeout(ctx, issuerDomainRecordsWait)
	defer cancelRecords()
	if out = r.waitFor(recordsCtx, out, hasRecords, "its DNS records", &resp.Diagnostics); resp.Diagnostics.HasError() {
		return
	}

	if plan.WaitForActive.ValueBool() {
		out = r.waitFor(ctx, out, isActive, "state ACTIVE", &resp.Diagnostics)
	}
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
	if out.State == issuerDomainDeleting {
		tflog.Warn(ctx, "issuer domain is being deleted outside Terraform; removing it from state", map[string]any{tfruntime.NameAttribute: out.Name})
		resp.State.RemoveResource(ctx)
		return
	}

	// An import has no configuration-only values yet.
	if state.WaitForActive.IsNull() {
		state.WaitForActive = types.BoolValue(false)
	}

	resp.Diagnostics.Append(state.fromDomain(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update has nothing to send: hostname and the parents replace the domain.
// It reads the domain back, and waits for ACTIVE when wait_for_active is set.
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

	timeout, d := plan.Timeouts.Update(ctx, issuerDomainDefaultTimeout)
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

	if plan.WaitForActive.ValueBool() {
		out = r.waitFor(ctx, out, isActive, "state ACTIVE", &resp.Diagnostics)
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

	timeout, d := state.Timeouts.Delete(ctx, issuerDomainDefaultTimeout)
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

// waitGone reads the domain until kit has removed it or ctx ends.
func (r *issuerDomainResource) waitGone(ctx context.Context, name string, timeout time.Duration, diags *diag.Diagnostics) {

	for {
		_, err := r.client.GetIssuerDomain(ctx, name)
		if aip.IsNotFound(err) {
			return
		}
		if err != nil && ctx.Err() == nil {
			diags.AddError("read issuer domain failed", err.Error())
			return
		}
		select {
		case <-ctx.Done():
			diags.AddError("issuer domain is still being deleted",
				fmt.Sprintf("%s is DELETING after %s: Authwise is removing its certificate and edge entries, and the hostname cannot be claimed again until that finishes. "+
					"It will finish on its own; raise timeouts.delete to wait longer.", name, timeout))
			return
		case <-time.After(r.poll):
		}
	}
}

// ImportState takes the full resource name; the parents read back from it.
func (r *issuerDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := issuerDomainScope.ParseName(issuerDomainCollection, req.ID); err != nil {
		resp.Diagnostics.AddError("invalid import ID for issuer domain", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), req.ID)...)
}

// issuerDomainCreateError words kit's refusal of CreateIssuerDomain by its
// ErrorInfo reason (domain authwise.io), which is the contract; the status
// message is prose and may change.
func issuerDomainCreateError(err error) (summary, detail string) {

	var info *errdetails.ErrorInfo
	for _, d := range status.Convert(err).Details() {
		if i, ok := d.(*errdetails.ErrorInfo); ok && i.GetDomain() == authwiseErrorDomain {
			info = i
		}
	}

	summary = "create issuer domain failed"
	if info == nil {
		return summary, err.Error()
	}
	summary = "create issuer domain refused: " + info.GetReason()

	switch info.GetReason() {
	case "HOSTNAME_CLAIMED":
		detail = "The hostname is already claimed in this Authwise install, by another issuer or tenant, or by a domain of yours that is still being deleted (a deleted domain releases its hostname only once its certificate and edge entries are gone)."
	case "HOSTNAME_APEX":
		detail = "The hostname is a zone apex, and a CNAME cannot sit at an apex. Use a subdomain such as login.<your domain>, or your DNS provider's CNAME flattening on a subdomain you point at it."
	case "HOSTNAME_IS_ISSUER":
		detail = "The hostname is an Authwise issuer host already; an issuer answers at its own host without a domain."
	case "PLAN_REQUIRED":
		detail = "Custom domains need a higher plan."
		if plan := info.GetMetadata()["plan"]; plan != "" {
			detail = fmt.Sprintf("Custom domains need the %s plan or above.", plan)
		}
	}

	return summary, strings.TrimSpace(detail + " " + err.Error())
}

// issuerDomainClient is issuerDomainAPI over kit's identity service.
type issuerDomainClient struct {
	client identitypb.AuthwiseIdentityServiceClient
}

func (c issuerDomainClient) CreateIssuerDomain(ctx context.Context, parent, hostname string) (*issuerDomain, error) {
	out, err := c.client.CreateIssuerDomain(ctx, &identitypb.CreateIssuerDomainRequest{
		Parent:       parent,
		IssuerDomain: &corepb.IssuerDomain{Hostname: hostname},
	})
	if err != nil {
		return nil, err
	}
	return issuerDomainFromProto(out), nil
}

func (c issuerDomainClient) GetIssuerDomain(ctx context.Context, name string) (*issuerDomain, error) {
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

// enumName is an enum value's name without its prefix; "" when unspecified.
func enumName(name, prefix string) string {
	name = strings.TrimPrefix(name, prefix)
	if name == "UNSPECIFIED" {
		return ""
	}
	return name
}

func issuerDomainFromProto(e *corepb.IssuerDomain) *issuerDomain {

	d := &issuerDomain{
		Name:     e.GetName(),
		Hostname: e.GetHostname(),
		State:    enumName(e.GetState().String(), "ISSUER_DOMAIN_STATE_"),
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
		d.DNSRecords = append(d.DNSRecords, issuerDomainRecord{Name: r.GetName(), Type: r.GetType(), Value: r.GetValue(), Purpose: r.GetPurpose()})
	}

	return d
}

func hasRecords(d *issuerDomain) bool { return len(d.DNSRecords) > 0 }

func isActive(d *issuerDomain) bool { return d.State == issuerDomainActive }

// waitFor reads the domain until done holds or ctx ends, and returns the
// last domain read. A FAILED domain ends the wait with its reason.
func (r *issuerDomainResource) waitFor(ctx context.Context, d *issuerDomain, done func(*issuerDomain) bool, what string, diags *diag.Diagnostics) *issuerDomain {

	for {
		if done(d) {
			return d
		}
		if d.State == issuerDomainFailed {
			diags.AddError("issuer domain failed", fmt.Sprintf("%s: %s (%s)", d.Hostname, d.FailureMessage, d.FailureCode))
			return d
		}
		select {
		case <-ctx.Done():
			diags.AddError("timed out waiting for the issuer domain",
				fmt.Sprintf("%s did not reach %s; it is %s. %s", d.Hostname, what, d.State, issuerDomainHint(d)))
			return d
		case <-time.After(r.poll):
		}
		next, err := r.client.GetIssuerDomain(ctx, d.Name)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			diags.AddError("read issuer domain failed", err.Error())
			return d
		}
		d = next
	}
}

// issuerDomainHint says what a stalled state is waiting on.
func issuerDomainHint(d *issuerDomain) string {
	switch d.State {
	case issuerDomainPendingDNS:
		return "The certificate authority has not seen the challenge record yet: check that it exists, is DNS-only, and has propagated."
	case issuerDomainVerifying, issuerDomainProvisioning:
		return "The certificate is being issued and attached; this can take several more minutes."
	case "":
		return "The domain controller has not picked the domain up yet."
	}
	return ""
}

func (m *issuerDomainModel) fromDomain(d *issuerDomain) diag.Diagnostics {

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
	m.FailureCode = types.StringValue(d.FailureCode)
	m.FailureMessage = types.StringValue(d.FailureMessage)
	m.FailureRecord = types.StringValue(d.FailureRecord)
	m.CertificateExpiresAt = types.StringValue("")
	if d.CertificateExpiresAt != nil {
		m.CertificateExpiresAt = types.StringValue(d.CertificateExpiresAt.UTC().Format(time.RFC3339))
	}

	recordType := types.ObjectType{AttrTypes: issuerDomainRecordAttrTypes()}
	records := make([]attr.Value, 0, len(d.DNSRecords))
	for _, rec := range d.DNSRecords {
		o, dd := types.ObjectValue(issuerDomainRecordAttrTypes(), map[string]attr.Value{
			tfruntime.NameAttribute: types.StringValue(rec.Name),
			recordTypeAttribute:     types.StringValue(rec.Type),
			recordValueAttribute:    types.StringValue(rec.Value),
			recordPurposeAttribute:  types.StringValue(rec.Purpose),
		})
		diags.Append(dd...)
		records = append(records, o)
	}
	list, dd := types.ListValue(recordType, records)
	diags.Append(dd...)
	m.DNSRecords = list

	return diags
}
