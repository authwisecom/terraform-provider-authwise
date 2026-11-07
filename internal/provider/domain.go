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
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
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
	productAttribute  = "product"
	customAttribute   = "custom"

	// productGuard is the one product with hosts (kit#903): a custom
	// Guard address such as guard.acme.com.
	productGuard = "guard"

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
	// Custom is a customer's hostname with the DNS tracking (kit#911).
	// Product is set on a product's custom host (kit#903). IssuerID is
	// what kit still reads out on a row claimed before kit#911; it names
	// nothing the provider writes.
	Custom         bool
	IssuerID       string
	Product        string
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

// custom reports whether the domain is a custom host, with the lifecycle:
// an issuer's or a product's.
func (d *domain) custom() bool { return d.Custom || d.IssuerID != "" || d.Product != "" }

// domainAPI is the slice of the identity service authwise_domain calls.
// Errors are gRPC statuses, as the identity client's are.
type domainAPI interface {
	// CreateDomain creates in.Hostname under parent, a custom host when
	// in.Custom or in.Product is set, with in.Labels.
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
	"HOSTNAME_IS_ISSUER":  "The hostname is an Authwise host already: bind the issuer to its native domain with authwise_issuer_domain_binding instead.",
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

// domainDeleteError words kit's refusal of a domain delete. DOMAIN_IN_USE
// (kit#911) names the issuers still bound to it.
func domainDeleteError(err error) (summary, detail string) {
	for _, d := range status.Convert(err).Details() {
		info, ok := d.(*errdetails.ErrorInfo)
		if !ok || info.GetDomain() != authwiseErrorDomain || info.GetReason() != "DOMAIN_IN_USE" {
			continue
		}
		return "delete domain refused: DOMAIN_IN_USE",
			fmt.Sprintf("Issuers are still bound to the domain (%s). Destroy their authwise_issuer_domain_binding resources first: "+
				"a binding whose domain_id is set from this domain is destroyed before it. %s", info.GetMetadata()["issuers"], err.Error())
	}
	return "delete domain failed", err.Error()
}

// requiresReplaceIfConfiguredBool replaces the resource when a configured
// value changes; an unset one follows the server.
func requiresReplaceIfConfiguredBool() planmodifier.Bool {
	return boolplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = !req.ConfigValue.IsNull()
	}, "Changing a configured value replaces the resource.", "Changing a configured value replaces the resource.")
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
)

// domainResource is authwise_domain (#47): a hostname the tenant serves.
// Native, or custom (a hostname of the customer's, with the DNS tracking);
// issuers reach either through authwise_issuer_domain_binding (tf#69,
// kit#911), and nothing on the domain names an issuer. Custom with
// product = guard (tf#67, kit#903), it serves the tenant's Guard address. A custom host's certificate is obtained and renewed on
// Authwise's edge. kit merged
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
	Custom               types.Bool     `tfsdk:"custom"`
	Product              types.String   `tfsdk:"product"`
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
		// 2 is kit#911's: custom in place of issuer_id. 1 is kit#808's
		// shape: the AWID as domain_id and the hostname beside it. 0 keyed
		// the domain by its hostname, in domain_id.
		Version: 2,
		MarkdownDescription: "A hostname of the tenant. A native host is an Authwise host such as `acme.id.authwise.com`. " +
			"A custom host (`custom = true`) is a hostname of yours, such as `login.acme.com`: Authwise obtains and renews its certificate, and you add two DNS records, " +
			"read from `dns_records`. Nothing on a domain names an issuer: an issuer answers at a host through an `authwise_issuer_domain_binding`, one per (domain, path), " +
			"so one host can serve several issuers at different paths and one issuer can answer at several hosts, each its own issuer URL (`iss`). " +
			"kit refuses to delete a domain while issuers are bound to it (`DOMAIN_IN_USE`).\n\n" +
			"A custom Guard address (`product = \"guard\"`) is a hostname of yours, such as `guard.acme.com`, that people type into the Guard client in place of the tenant's default address (`<slug>.guard.<estate>`). " +
			"Its `traffic` record points at that default address, so the tenant needs a slug first, set on the tenant in kit. It is never an issuer URL: people still sign in at the issuer's own host. It takes the same records, waits and activation as an issuer's custom host.\n\n" +
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
				MarkdownDescription: "Server-assigned resource id (`d-…`), the last segment of `name`: what `authwise_issuer_domain_binding.domain_id` takes.",
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
			customAttribute: schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "A hostname of yours, such as `login.acme.com`, rather than an Authwise host: Authwise obtains its certificate, and `dns_records` lists the records to add. " +
					"Set on create; changing a configured value replaces the domain. A custom Guard address (`product`) is custom without setting it. " +
					"kit gates custom hosts by plan (`PLAN_REQUIRED`).",
				PlanModifiers: []planmodifier.Bool{requiresReplaceIfConfiguredBool(), boolplanmodifier.UseStateForUnknown()},
			},
			productAttribute: schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The product a custom host serves: `guard`, a custom Guard address. No issuer can be bound to it. Changing it replaces the domain. " +
					"kit gates it by plan like an issuer's custom host (`PLAN_REQUIRED`); a trial has the default address only.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf(productGuard)},
			},
			labelsAttribute: schema.MapAttribute{
				Optional:      true,
				Computed:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"dns_records": schema.ListNestedAttribute{
				Computed: true,
				MarkdownDescription: "A custom host's records to add at your DNS provider, one per `purpose`: `traffic` (the hostname, a CNAME to the install's edge name, or to the tenant's default Guard address) and `challenge` (the certificate authority's proof of control). " +
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
				MarkdownDescription: "Where a custom host's `traffic` record points: the Authwise install's edge name (kit#911). A host claimed before kit#911 keeps the issuer host it recorded. Empty on a native host and on a custom Guard address, whose target is the `traffic` record's value.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"state": computed("A custom host's state, empty on a native host: `PENDING_DNS` (waiting for the challenge record), `VERIFYING` (issuing the certificate), `PROVISIONING` (attaching it; the hostname routes), `ACTIVE`, " +
				"`DEGRADED` (serving, but the challenge record is gone and renewal will fail), `FAILED` (see `failure_code`) or `DELETING`."),
			"failure_code": computed("Why the domain is `FAILED` or `DEGRADED`; empty otherwise. One of `DNS_NOT_FOUND` (a record is missing), `CAA` (a CAA record excludes the certificate authority), " +
				"`CONFIG` (a record exists with the wrong content, or is proxied), `RATE_LIMITED` (the certificate authority's limit; it retries), `PROBE_FAILED` (the hostname did not answer as Authwise or the product) or `PROVIDER_ERROR` (Authwise's side)."),
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

// ValidateConfig holds a custom host (custom or product set) to an FQDN
// without a port, and a native one to a lower-case host.
func (r *domainResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var hostname, product types.String
	var customFlag types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(hostnameAttribute), &hostname)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(customAttribute), &customFlag)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(productAttribute), &product)...)
	if resp.Diagnostics.HasError() || !known(hostname, customFlag, product) || hostname.IsNull() {
		return
	}

	h := hostname.ValueString()
	custom := customFlag.ValueBool() || !product.IsNull()
	switch {
	case custom && !hostnamePattern.MatchString(h):
		resp.Diagnostics.AddAttributeError(path.Root(hostnameAttribute), "invalid custom hostname",
			"must be a lower-case fully qualified hostname such as login.acme.com or guard.acme.com, without a scheme, port, path or trailing dot")
	case !custom && !nativeHostnamePattern.MatchString(h):
		resp.Diagnostics.AddAttributeError(path.Root(hostnameAttribute), "invalid hostname",
			"must be a lower-case hostname such as acme.id.authwise.com, optionally with a port, without a scheme, path or trailing dot")
	}
}

// known reports whether no value is unknown.
func known(values ...attr.Value) bool {
	for _, v := range values {
		if v.IsUnknown() {
			return false
		}
	}
	return true
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

	in := &domain{Hostname: plan.Hostname.ValueString(), Custom: plan.Custom.ValueBool(), Product: plan.Product.ValueString()}
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
// custom, product and the tenant replace the domain. With labels unchanged it
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
// the hostname when this returns. A native host is gone at once. kit
// refuses either while an issuer is bound to it (DOMAIN_IN_USE, kit#911).
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
		summary, detail := domainDeleteError(err)
		resp.Diagnostics.AddError(summary, detail)
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
	m.Custom = types.BoolValue(d.custom())
	m.Product = types.StringNull()
	if d.Product != "" {
		m.Product = types.StringValue(d.Product)
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
		Name: null, DomainID: null, TenantID: null, Hostname: null, Custom: types.BoolNull(), Product: null,
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
func (r *domainResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
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
		1: {
			PriorSchema: domainSchemaV1(ctx),
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {

				var prior domainModelV1
				resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
				if resp.Diagnostics.HasError() {
					return
				}

				// A host claimed with issuer_id was custom; the refresh that
				// follows reads custom from kit either way.
				m := domainModel{
					Name: prior.Name, DomainID: prior.DomainID, TenantID: prior.TenantID, Hostname: prior.Hostname,
					Custom:  types.BoolValue(!prior.IssuerID.IsNull() || !prior.Product.IsNull() || prior.State.ValueString() != ""),
					Product: prior.Product, Labels: prior.Labels, DNSRecords: prior.DNSRecords, TrafficTarget: prior.TrafficTarget,
					State: prior.State, FailureCode: prior.FailureCode, FailureMessage: prior.FailureMessage, FailureRecord: prior.FailureRecord,
					CertificateExpiresAt: prior.CertificateExpiresAt, ActivatedAt: prior.ActivatedAt, Timeouts: prior.Timeouts,
				}
				resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			},
		},
	}
}

// domainModelV1 is authwise_domain's state from kit#808 to kit#911: issuer_id
// where custom is now.
type domainModelV1 struct {
	Name                 types.String   `tfsdk:"name"`
	DomainID             types.String   `tfsdk:"domain_id"`
	TenantID             types.String   `tfsdk:"tenant_id"`
	Hostname             types.String   `tfsdk:"hostname"`
	IssuerID             types.String   `tfsdk:"issuer_id"`
	Product              types.String   `tfsdk:"product"`
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

// domainSchemaV1 is the schema domainModelV1 was written with: today's,
// with issuer_id in place of custom.
func domainSchemaV1(ctx context.Context) *schema.Schema {
	s := domainSchema(ctx)
	s.Version = 1
	attrs := make(map[string]schema.Attribute, len(s.Attributes))
	for k, v := range s.Attributes {
		attrs[k] = v
	}
	delete(attrs, customAttribute)
	attrs[issuerIDAttribute] = schema.StringAttribute{Optional: true}
	s.Attributes = attrs
	return &s
}
