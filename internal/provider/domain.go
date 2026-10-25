package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

const (
	// domainTypeName is the Terraform type of the generated domain resource,
	// which this one replaces; the generated data sources stay.
	domainTypeName   = "authwise_domain"
	domainCollection = "domains"

	// domainsClientKey is the provider data key of the domainAPI.
	domainsClientKey = "domains"

	domainIDAttribute = "domain_id"
	hostnameAttribute = "hostname"
	issuerIDAttribute = "issuer_id"

	// A DNS record's attributes.
	recordTypeAttribute    = "type"
	recordValueAttribute   = "value"
	recordPurposeAttribute = "purpose"

	// authwiseErrorDomain is the ErrorInfo domain of kit's refusals.
	authwiseErrorDomain = "authwise.io"
)

// The states kit reports for a custom host (CUSTOM_DOMAINS §4.1), without
// the enum prefix. A native host has none.
const (
	domainPendingDNS   = "PENDING_DNS"
	domainVerifying    = "VERIFYING"
	domainProvisioning = "PROVISIONING"
	domainActive       = "ACTIVE"
	domainDegraded     = "DEGRADED"
	domainFailed       = "FAILED"
	domainDeleting     = "DELETING"
)

const (
	// domainRecordsWait bounds the wait for the DNS records: one
	// controller round trip, typically under thirty seconds.
	domainRecordsWait = 5 * time.Minute
	// domainDefaultTimeout is the create, update and delete timeout
	// when the configuration sets none. Certificate issuance and the cloud
	// side's teardown are both minutes, not seconds.
	domainDefaultTimeout = 30 * time.Minute
	domainPollInterval   = 5 * time.Second
)

var domainScope = aip.NewScope("tenants")

// domainNamePattern is a domain's full resource name.
var domainNamePattern = regexp.MustCompile(`^tenants/[^/]+/domains/[^/]+$`)

// domainIDPattern is a domain's AWID. Before kit#808 a domain was keyed by
// its hostname, and a name whose last segment is not one is state written
// then, or an import by hostname: Read finds the row by hostname.
var domainIDPattern = regexp.MustCompile(`^d-[0-9a-z]+$`)

// hostnamePattern is a lower-case FQDN of two or more labels, without a
// trailing dot: a custom host. kit is the authority (it refuses an apex, a
// claimed name and anything below the plan gate); this only catches a URL
// or a stray capital in plan.
var hostnamePattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// nativeHostnamePattern is a native host: lower case, one label or more,
// and a port allowed (kit#336), as a development install serves on one.
var nativeHostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

// domain is kit's Domain as these resources use it, and IssuerDomain for
// the deprecated aliases: the enums as their names without the prefix. The
// clients are the only code that sees the proto, so the waits are tested
// against a plain fake.
type domain struct {
	Name     string
	Hostname string
	// IssuerID is set on a custom host and "" on a native one.
	IssuerID       string
	Labels         map[string]string
	State          string
	TrafficTarget  string
	DNSRecords     []domainRecord
	FailureCode    string
	FailureMessage string
	// FailureRecord is the record purpose the failure is about, or "".
	FailureRecord        string
	CertificateExpiresAt *time.Time
	ActivatedAt          *time.Time
}

type domainRecord struct {
	Name, Type, Value, Purpose string
}

// custom reports whether the domain is a custom host, with the lifecycle.
func (d *domain) custom() bool { return d.IssuerID != "" }

// domainAPI is the slice of the identity service authwise_domain calls.
// Errors are gRPC statuses, as the identity client's are.
type domainAPI interface {
	// CreateDomain creates in.Hostname under parent, a custom host when
	// in.IssuerID is set, with in.Labels.
	CreateDomain(ctx context.Context, parent string, in *domain) (*domain, error)
	GetDomain(ctx context.Context, name string) (*domain, error)
	// FindDomain is the domain under parent with hostname, or NotFound.
	FindDomain(ctx context.Context, parent, hostname string) (*domain, error)
	UpdateDomainLabels(ctx context.Context, name string, labels map[string]string) (*domain, error)
	DeleteDomain(ctx context.Context, name string) error
}

// domainPoller reads a domain until the controller has moved it on. Every
// domain resource shares it, the deprecated aliases with their own get.
type domainPoller struct {
	get func(ctx context.Context, name string) (*domain, error)
	// poll is the interval between reads while waiting.
	poll time.Duration
}

func (p *domainPoller) configured(diags *diag.Diagnostics) bool {
	if p.get == nil {
		diags.AddError("the domain resource is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

// providerClient takes the client under key from the provider's data, and
// returns that data; nil when there is none yet or it is not the provider's.
func providerClient[C any](req resource.ConfigureRequest, key string, diags *diag.Diagnostics) (C, *tfruntime.ProviderData) {

	var zero C
	if req.ProviderData == nil {
		return zero, nil
	}

	pd, ok := req.ProviderData.(*tfruntime.ProviderData)
	if !ok {
		diags.AddError("unexpected provider data", fmt.Sprintf("expected *tf.ProviderData, got %T", req.ProviderData))
		return zero, nil
	}

	client, ok := pd.Clients[key].(C)
	if !ok {
		diags.AddError("missing client", fmt.Sprintf("provider data key %q is not a %T", key, zero))
		return zero, nil
	}

	return client, pd
}

func hasRecords(d *domain) bool { return len(d.DNSRecords) > 0 }

// routes holds once kit routes the hostname to the issuer: the certificate is
// issued and attached, so a traffic record can go in.
func routes(d *domain) bool {
	return d.State == domainProvisioning || d.State == domainActive
}

// waitFor reads the domain until done holds or ctx ends, and returns the
// last domain read. A FAILED domain ends the wait with its reason.
func (p *domainPoller) waitFor(ctx context.Context, d *domain, done func(*domain) bool, what string, diags *diag.Diagnostics) *domain {

	for {
		if done(d) {
			return d
		}
		if d.State == domainFailed {
			diags.AddError("domain failed", fmt.Sprintf("%s: %s (%s)", d.Hostname, d.FailureMessage, d.FailureCode))
			return d
		}
		select {
		case <-ctx.Done():
			diags.AddError("timed out waiting for the domain",
				fmt.Sprintf("%s did not reach %s; it is %s. %s", d.Hostname, what, d.State, domainHint(d)))
			return d
		case <-time.After(p.poll):
		}
		next, err := p.get(ctx, d.Name)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			diags.AddError("read domain failed", err.Error())
			return d
		}
		d = next
	}
}

// waitGone reads the domain until kit has removed it or ctx ends.
func (p *domainPoller) waitGone(ctx context.Context, name string, timeout time.Duration, diags *diag.Diagnostics) {

	for {
		_, err := p.get(ctx, name)
		if aip.IsNotFound(err) {
			return
		}
		if err != nil && ctx.Err() == nil {
			diags.AddError("read domain failed", err.Error())
			return
		}
		select {
		case <-ctx.Done():
			diags.AddError("domain is still being deleted",
				fmt.Sprintf("%s is DELETING after %s: Authwise is removing its certificate and edge entries, and the hostname cannot be claimed again until that finishes. "+
					"It will finish on its own; raise timeouts.delete to wait longer.", name, timeout))
			return
		case <-time.After(p.poll):
		}
	}
}

// domainHint says what a stalled state is waiting on.
func domainHint(d *domain) string {
	switch d.State {
	case domainPendingDNS:
		return "The certificate authority has not seen the challenge record yet: check that it exists, is DNS-only, and has propagated."
	case domainVerifying, domainProvisioning:
		return "The certificate is being issued and attached; this can take several more minutes."
	case "":
		return "The domain controller has not picked the domain up yet."
	}
	return ""
}

// domainRefusals is the remedy for each ErrorInfo reason create is
// refused with.
var domainRefusals = map[string]string{
	"HOSTNAME_CLAIMED":    "The hostname is already claimed in this Authwise install, by another issuer or tenant, or by a domain of yours that is still being deleted (a deleted domain releases its hostname only once its certificate and edge entries are gone).",
	"HOSTNAME_APEX":       "The hostname is a zone apex, and a CNAME cannot sit at an apex. Use a subdomain such as login.<your domain>, or your DNS provider's CNAME flattening on a subdomain you point at it.",
	"HOSTNAME_IS_ISSUER":  "The hostname is an Authwise issuer host already; an issuer answers at its own host without a domain.",
	"PLAN_REQUIRED":       "Custom domains need a higher plan.",
	"DOMAINS_UNAVAILABLE": "This Authwise install has no domain controller, so a domain added to it would never get a certificate. Custom domains are not available on it.",
}

// domainCreateError words kit's refusal of a domain create by its
// ErrorInfo reason (domain authwise.io), which is the contract; the status
// message is prose and may change.
func domainCreateError(err error) (summary, detail string) {

	var info *errdetails.ErrorInfo
	for _, d := range status.Convert(err).Details() {
		if i, ok := d.(*errdetails.ErrorInfo); ok && i.GetDomain() == authwiseErrorDomain {
			info = i
		}
	}

	summary = "create domain failed"
	if info == nil {
		return summary, err.Error()
	}
	summary = "create domain refused: " + info.GetReason()

	detail = domainRefusals[info.GetReason()]
	if plan := info.GetMetadata()["plan"]; info.GetReason() == "PLAN_REQUIRED" && plan != "" {
		// kit sends the enum name, BUSINESS.
		detail = fmt.Sprintf("Custom domains need the %s%s plan or above.", plan[:1], strings.ToLower(plan[1:]))
	}

	return summary, strings.TrimSpace(detail + " " + err.Error())
}

// enumName is an enum value's name without its prefix; "" when unspecified.
func enumName(name, prefix string) string {
	name = strings.TrimPrefix(name, prefix)
	if name == "UNSPECIFIED" {
		return ""
	}
	return name
}

func domainRecordAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		tfruntime.NameAttribute: types.StringType,
		recordTypeAttribute:     types.StringType,
		recordValueAttribute:    types.StringType,
		recordPurposeAttribute:  types.StringType,
	}
}

// domainRecordsValue is records as the dns_records list.
func domainRecordsValue(records []domainRecord) (types.List, diag.Diagnostics) {

	var diags diag.Diagnostics

	values := make([]attr.Value, 0, len(records))
	for _, rec := range records {
		o, d := types.ObjectValue(domainRecordAttrTypes(), map[string]attr.Value{
			tfruntime.NameAttribute: types.StringValue(rec.Name),
			recordTypeAttribute:     types.StringValue(rec.Type),
			recordValueAttribute:    types.StringValue(rec.Value),
			recordPurposeAttribute:  types.StringValue(rec.Purpose),
		})
		diags.Append(d...)
		values = append(values, o)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: domainRecordAttrTypes()}, values)
	diags.Append(d...)

	return list, diags
}

// timestampValue is t in RFC 3339, or "" when unset.
func timestampValue(t *time.Time) types.String {
	if t == nil {
		return types.StringValue("")
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}

// domainTimeoutsAttrTypes are the attributes of a timeouts block with
// create, update and delete.
func domainTimeoutsAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType}
}

var (
	_ resource.Resource                   = &domainResource{}
	_ resource.ResourceWithConfigure      = &domainResource{}
	_ resource.ResourceWithImportState    = &domainResource{}
	_ resource.ResourceWithValidateConfig = &domainResource{}
	_ resource.ResourceWithUpgradeState   = &domainResource{}
	_ resource.ResourceWithMoveState      = &domainResource{}
)

// domainResource is authwise_domain (#47): a hostname the tenant serves
// logins on. Native (no issuer_id), an issuer names it as its host; custom
// (issuer_id set), it is a hostname of the customer's serving that issuer,
// with the certificate obtained and renewed on Authwise's edge. kit merged
// its IssuerDomain into Domain (kit#808), keyed by an AWID where it was
// keyed by the hostname. Hand-written because a custom host's Create and
// Delete wait on the domain controller, as authwise_issuer_domain's did.
type domainResource struct {
	domainPoller
	client   domainAPI
	defaults map[string]string
}

type domainModel struct {
	Name                 types.String   `tfsdk:"name"`
	DomainID             types.String   `tfsdk:"domain_id"`
	TenantID             types.String   `tfsdk:"tenant_id"`
	Hostname             types.String   `tfsdk:"hostname"`
	IssuerID             types.String   `tfsdk:"issuer_id"`
	Labels               types.Map      `tfsdk:"labels"`
	DNSRecords           types.List     `tfsdk:"dns_records"`
	TrafficTarget        types.String   `tfsdk:"traffic_target"`
	State                types.String   `tfsdk:"state"`
	FailureCode          types.String   `tfsdk:"failure_code"`
	FailureMessage       types.String   `tfsdk:"failure_message"`
	FailureRecord        types.String   `tfsdk:"failure_record"`
	CertificateExpiresAt types.String   `tfsdk:"certificate_expires_at"`
	ActivatedAt          types.String   `tfsdk:"activated_at"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

func newDomainResource() resource.Resource {
	return &domainResource{domainPoller: domainPoller{poll: domainPollInterval}}
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = domainSchema(ctx)
}

func domainSchema(ctx context.Context) schema.Schema {

	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}

	return schema.Schema{
		// 1 is kit#808's shape: the AWID as domain_id and the hostname
		// beside it. 0 keyed the domain by its hostname, in domain_id.
		Version: 1,
		MarkdownDescription: "A hostname the tenant serves logins on, of one of two kinds.\n\n" +
			"- **Native** (no `issuer_id`): an Authwise host such as `acme.id.authwise.com`. An issuer names it as its host (`authwise_issuer.domain_id`).\n" +
			"- **Custom** (`issuer_id` set): a hostname of yours that serves that issuer, such as `login.acme.com`. Authwise obtains and renews its certificate; you add two DNS records, " +
			"read from `dns_records`. The issuer keeps answering at its own host as well, and each host is its own issuer URL (`iss`).\n\n" +
			"For a custom host, create returns once the records are known, so DNS resources can take them in the same apply. Add the challenge record first and point traffic only once `state` is `PROVISIONING` or `ACTIVE`, " +
			"so the certificate exists before any request arrives: `authwise_domain_activation` waits for that between the two records, in one apply. Both records must be DNS-only (on Cloudflare, `proxied = false`), and the challenge record stays for as long as the domain does: renewal depends on it. " +
			"Destroying a custom host waits until Authwise has released the hostname, which takes the cloud side's teardown.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full resource name (`tenants/{t}/domains/{d}`); serves as the Terraform ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			domainIDAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned resource id (`d-…`), the last segment of `name`: what `authwise_issuer.domain_id` takes.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Parent identifier `tenant_id`; overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.",
				PlanModifiers:       []planmodifier.String{tfruntime.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			hostnameAttribute: schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The hostname, lower case, without a scheme or trailing dot. Unique across Authwise; kit refuses one already claimed. Changing it replaces the domain. " +
					"A custom host must be a fully qualified subdomain, since a CNAME cannot sit at a zone apex; a native one may carry a port.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			issuerIDAttribute: schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The issuer a custom host serves (`i-…`); unset for a native host. Changing it replaces the domain. " +
					"Set it from the issuer resource, so Terraform destroys the domain before the issuer: kit refuses to delete an issuer with a domain on it (`ISSUER_HAS_DOMAINS`).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{tfruntime.ReferenceID("i", "issuer", "authwise_issuer.<name>.issuer_id")},
			},
			"labels": schema.MapAttribute{
				Optional:      true,
				Computed:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"dns_records": schema.ListNestedAttribute{
				Computed: true,
				MarkdownDescription: "A custom host's records to add at your DNS provider, one per `purpose`: `traffic` (the hostname, a CNAME to the issuer's own host) and `challenge` (the certificate authority's proof of control). " +
					"Known after create; empty on a native host. Key DNS resources on `purpose`, not on these values, so the keys are known at plan; see the example.",
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
				MarkdownDescription: "A custom host's issuer's own Authwise host, such as `example.id.authwise.com`: the value of the `traffic` record.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"state": computed("A custom host's state, empty on a native host: `PENDING_DNS` (waiting for the challenge record), `VERIFYING` (issuing the certificate), `PROVISIONING` (attaching it; the hostname routes), `ACTIVE`, " +
				"`DEGRADED` (serving, but the challenge record is gone and renewal will fail), `FAILED` (see `failure_code`) or `DELETING`."),
			"failure_code": computed("Why the domain is `FAILED` or `DEGRADED`; empty otherwise. One of `DNS_NOT_FOUND` (a record is missing), `CAA` (a CAA record excludes the certificate authority), " +
				"`CONFIG` (a record exists with the wrong content, or is proxied), `RATE_LIMITED` (the certificate authority's limit; it retries), `PROBE_FAILED` (the hostname did not answer as the issuer) or `PROVIDER_ERROR` (Authwise's side)."),
			"failure_message":        computed("`failure_code` in words, with the record or setting to fix."),
			"failure_record":         computed("The `purpose` of the record the failure is about, `traffic` or `challenge`; empty when it is about neither."),
			"certificate_expires_at": computed("When a custom host's current certificate expires, RFC 3339; empty before the first is issued."),
			"activated_at":           computed("When a custom host first became `ACTIVE`, RFC 3339; empty before then."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *domainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, pd := providerClient[domainAPI](req, domainsClientKey, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client, r.get, r.defaults = client, client.GetDomain, pd.Defaults
}

// ValidateConfig holds a custom host to an FQDN without a port, and a
// native one to a lower-case host.
func (r *domainResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var hostname, issuerID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(hostnameAttribute), &hostname)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(issuerIDAttribute), &issuerID)...)
	if resp.Diagnostics.HasError() || hostname.IsNull() || hostname.IsUnknown() || issuerID.IsUnknown() {
		return
	}

	h := hostname.ValueString()
	switch {
	case !issuerID.IsNull() && !hostnamePattern.MatchString(h):
		resp.Diagnostics.AddAttributeError(path.Root(hostnameAttribute), "invalid custom hostname",
			"must be a lower-case fully qualified hostname such as login.acme.com, without a scheme, port, path or trailing dot")
	case issuerID.IsNull() && !nativeHostnamePattern.MatchString(h):
		resp.Diagnostics.AddAttributeError(path.Root(hostnameAttribute), "invalid hostname",
			"must be a lower-case hostname such as acme.id.authwise.com, optionally with a port, without a scheme, path or trailing dot")
	}
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan domainModel
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

	parent, err := r.parent(plan.TenantID)
	if err != nil {
		resp.Diagnostics.AddError("cannot resolve parent for domain", err.Error()+"; set it on the resource or as a provider default")
		return
	}

	in := &domain{Hostname: plan.Hostname.ValueString(), IssuerID: plan.IssuerID.ValueString()}
	if !plan.Labels.IsUnknown() {
		resp.Diagnostics.Append(plan.Labels.ElementsAs(ctx, &in.Labels, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	out, err := r.client.CreateDomain(ctx, parent, in)
	if err != nil {
		summary, detail := domainCreateError(err)
		resp.Diagnostics.AddAttributeError(path.Root(hostnameAttribute), summary, detail)
		return
	}

	// The row exists from here: state is saved even when a wait below
	// fails, so Terraform taints the domain rather than losing it.
	defer func() {
		resp.Diagnostics.Append(plan.fromDomain(out)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}()

	if !out.custom() {
		return
	}
	recordsCtx, cancelRecords := context.WithTimeout(ctx, domainRecordsWait)
	defer cancelRecords()
	out = r.waitFor(recordsCtx, out, hasRecords, "its DNS records", &resp.Diagnostics)
}

func (r *domainResource) parent(tenantID types.String) (string, error) {
	t := r.defaults[tenantIDAttribute]
	if s := tenantID.ValueString(); s != "" {
		t = s
	}
	return domainScope.ComposeParent(map[string]string{tenantIDAttribute: t})
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.read(ctx, state.Name.ValueString())
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read domain failed", err.Error())
		return
	}

	// A custom host someone else deleted is gone for Terraform's purposes:
	// it no longer routes, and kit removes the row once the cloud side is.
	if out.State == domainDeleting {
		tflog.Warn(ctx, "domain is being deleted outside Terraform; removing it from state", map[string]any{tfruntime.NameAttribute: out.Name})
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(state.fromDomain(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// read gets the domain by name. A name keyed by hostname, from state
// written before kit#808 or an import by hostname, is found by hostname
// under its tenant, and the domain read back carries its AWID name.
func (r *domainResource) read(ctx context.Context, name string) (*domain, error) {

	ids, id, err := domainScope.ParseName(domainCollection, name)
	if err != nil {
		return nil, err
	}
	if domainIDPattern.MatchString(id) {
		return r.client.GetDomain(ctx, name)
	}

	parent, err := domainScope.ComposeParent(ids)
	if err != nil {
		return nil, err
	}
	return r.client.FindDomain(ctx, parent, id)
}

// Update sends labels, the one attribute that changes in place; hostname,
// issuer_id and the tenant replace the domain. With labels unchanged it
// reads the domain back.
func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state domainModel
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

	name := state.Name.ValueString()

	var out *domain
	var err error
	if plan.Labels.IsUnknown() || plan.Labels.Equal(state.Labels) {
		out, err = r.client.GetDomain(ctx, name)
	} else {
		labels := map[string]string{}
		resp.Diagnostics.Append(plan.Labels.ElementsAs(ctx, &labels, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		out, err = r.client.UpdateDomainLabels(ctx, name, labels)
	}
	if err != nil {
		resp.Diagnostics.AddError("update domain failed", err.Error())
		return
	}

	resp.Diagnostics.Append(plan.fromDomain(out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is two-phase on the server for a custom host: the row goes
// DELETING and kit removes it once the controller reports the cloud side
// gone. Waiting for that means a replacement, or another tenant, can claim
// the hostname when this returns. A native host is gone at once, and kit
// refuses it while an issuer names it (DOMAIN_HAS_ISSUERS).
func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state domainModel
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

	if err := r.client.DeleteDomain(ctx, name); err != nil {
		if aip.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("delete domain failed", err.Error())
		return
	}

	r.waitGone(ctx, name, timeout, &resp.Diagnostics)
}

// ImportState takes the full resource name, tenants/{t}/domains/{d}, or
// tenants/{t}/domains/{hostname}, which Read resolves to the AWID.
func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := domainScope.ParseName(domainCollection, req.ID); err != nil {
		resp.Diagnostics.AddError("invalid import ID for domain", err.Error()+"; expected tenants/{t}/domains/{d} or tenants/{t}/domains/{hostname}")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tfruntime.NameAttribute), req.ID)...)
}

func (m *domainModel) fromDomain(d *domain) diag.Diagnostics {

	var diags diag.Diagnostics

	ids, id, err := domainScope.ParseName(domainCollection, d.Name)
	if err != nil {
		diags.AddError("unexpected domain name", err.Error())
		return diags
	}

	m.Name = types.StringValue(d.Name)
	m.DomainID = types.StringValue(id)
	m.TenantID = types.StringValue(ids[tenantIDAttribute])
	m.Hostname = types.StringValue(d.Hostname)
	m.IssuerID = types.StringNull()
	if d.custom() {
		m.IssuerID = types.StringValue(d.IssuerID)
	}
	labels := make(map[string]attr.Value, len(d.Labels))
	for k, v := range d.Labels {
		labels[k] = types.StringValue(v)
	}
	var dd diag.Diagnostics
	m.Labels, dd = types.MapValue(types.StringType, labels)
	diags.Append(dd...)
	m.State = types.StringValue(d.State)
	m.TrafficTarget = types.StringValue(d.TrafficTarget)
	m.FailureCode = types.StringValue(d.FailureCode)
	m.FailureMessage = types.StringValue(d.FailureMessage)
	m.FailureRecord = types.StringValue(d.FailureRecord)
	m.CertificateExpiresAt = timestampValue(d.CertificateExpiresAt)
	m.ActivatedAt = timestampValue(d.ActivatedAt)
	m.DNSRecords, dd = domainRecordsValue(d.DNSRecords)
	diags.Append(dd...)

	return diags
}

// emptyDomainModel is a model with every computed attribute null, the
// timeouts block among them, for the state upgrade and move to fill in.
func emptyDomainModel() domainModel {
	null := types.StringNull()
	return domainModel{
		Name: null, DomainID: null, TenantID: null, Hostname: null, IssuerID: null,
		Labels:        types.MapNull(types.StringType),
		DNSRecords:    types.ListNull(types.ObjectType{AttrTypes: domainRecordAttrTypes()}),
		TrafficTarget: null, State: null, FailureCode: null, FailureMessage: null, FailureRecord: null,
		CertificateExpiresAt: null, ActivatedAt: null,
		Timeouts: timeouts.Value{Object: types.ObjectNull(domainTimeoutsAttrTypes())},
	}
}

// domainModelV0 is authwise_domain's state before kit#808, when the
// generated resource keyed the domain by its hostname, in domain_id.
type domainModelV0 struct {
	Name     types.String `tfsdk:"name"`
	DomainID types.String `tfsdk:"domain_id"`
	TenantID types.String `tfsdk:"tenant_id"`
	Labels   types.Map    `tfsdk:"labels"`
	Config   types.String `tfsdk:"config"`
}

// UpgradeState carries state from 0.18 and earlier: the hostname was the
// id. The upgraded state keeps the hostname-keyed name, which the refresh
// that follows resolves to the AWID (Read), so the upgrade needs no API.
// config is dropped: DomainConfig has no fields.
func (r *domainResource) UpgradeState(context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema: &schema.Schema{
				Attributes: map[string]schema.Attribute{
					tfruntime.NameAttribute: schema.StringAttribute{Computed: true},
					domainIDAttribute:       schema.StringAttribute{Required: true},
					tenantIDAttribute:       schema.StringAttribute{Optional: true, Computed: true},
					"labels":                schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
					"config":                schema.StringAttribute{Optional: true, Computed: true},
				},
			},
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {

				var prior domainModelV0
				resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
				if resp.Diagnostics.HasError() {
					return
				}

				m := emptyDomainModel()
				m.Name, m.DomainID, m.TenantID, m.Labels = prior.Name, prior.DomainID, prior.TenantID, prior.Labels
				m.Hostname = prior.DomainID
				resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			},
		},
	}
}

// MoveState takes authwise_issuer_domain's state in a moved block (Terraform
// 1.8 and later): the same row, now under tenants/{t}/domains/{d} with the
// same id. The refresh that follows reads the rest.
func (r *domainResource) MoveState(ctx context.Context) []resource.StateMover {

	source := &resource.SchemaResponse{}
	(&issuerDomainResource{}).Schema(ctx, resource.SchemaRequest{}, source)

	return []resource.StateMover{{
		SourceSchema: &source.Schema,
		StateMover: func(ctx context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {

			if req.SourceTypeName != "authwise_issuer_domain" {
				return
			}

			var prior issuerDomainModel
			resp.Diagnostics.Append(req.SourceState.Get(ctx, &prior)...)
			if resp.Diagnostics.HasError() {
				return
			}

			name, err := domainMovedName(prior.Name.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("unexpected issuer domain name", err.Error())
				return
			}

			m := emptyDomainModel()
			m.Name = types.StringValue(name)
			m.DomainID, m.TenantID, m.IssuerID, m.Hostname = prior.IssuerDomainID, prior.TenantID, prior.IssuerID, prior.Hostname
			m.DNSRecords, m.TrafficTarget, m.State = prior.DNSRecords, prior.TrafficTarget, prior.State
			m.FailureCode, m.FailureMessage, m.FailureRecord = prior.FailureCode, prior.FailureMessage, prior.FailureRecord
			m.CertificateExpiresAt = prior.CertificateExpiresAt
			m.Timeouts = prior.Timeouts
			resp.Diagnostics.Append(resp.TargetState.Set(ctx, &m)...)
		},
	}}
}

// domainMovedName is an issuer domain's name as the domain's: the same id
// under the tenant.
func domainMovedName(issuerDomainName string) (string, error) {
	ids, id, err := issuerDomainScope.ParseName(issuerDomainCollection, issuerDomainName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tenants/%s/domains/%s", ids[tenantIDAttribute], id), nil
}
