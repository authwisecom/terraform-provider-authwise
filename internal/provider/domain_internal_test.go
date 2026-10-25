package provider

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeDomains plays kit and the domain controller over one set of rows,
// served as domains and, for a custom host, through the deprecated
// issuer-domains RPCs, as kit's aliases serve the merged table (kit#808).
// A created custom host has no records until reads have passed
// recordsAfter, and turns ACTIVE after activeAfter; a deleted one stays
// DELETING for goneAfter reads. With holdProvisioning it stops at
// PROVISIONING instead, as a real one does until the controller finds the
// traffic record in public DNS. A native host has no lifecycle.
type fakeDomains struct {
	mu sync.Mutex
	// rows are keyed by id, each named tenants/{t}/domains/{id}.
	rows  map[string]*domain
	reads map[string]int
	seq   int

	recordsAfter, activeAfter, goneAfter int
	// fail, when set, is the code a domain fails with instead of issuing.
	fail string
	// holdProvisioning keeps an issued domain at PROVISIONING.
	holdProvisioning bool
}

func newFakeDomains() *fakeDomains {
	return &fakeDomains{rows: map[string]*domain{}, reads: map[string]int{}, recordsAfter: 2, activeAfter: 4, goneAfter: 2}
}

func lastSegment(name string) string { return name[strings.LastIndex(name, "/")+1:] }

func copyDomain(d *domain) *domain {
	cp := *d
	cp.Labels = maps.Clone(d.Labels)
	cp.DNSRecords = append([]domainRecord(nil), d.DNSRecords...)
	return &cp
}

func (f *fakeDomains) CreateDomain(_ context.Context, parent string, in *domain) (*domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.rows {
		if d.Hostname == in.Hostname {
			return nil, status.Errorf(codes.AlreadyExists, "%s is already claimed", in.Hostname)
		}
	}
	f.seq++
	id := fmt.Sprintf("d-%d", f.seq)
	d := &domain{Name: parent + "/domains/" + id, Hostname: in.Hostname, IssuerID: in.IssuerID, Labels: maps.Clone(in.Labels)}
	if d.custom() {
		d.State, d.TrafficTarget = domainPendingDNS, "acme.id.authwise.test"
	}
	f.rows[id] = d
	return copyDomain(d), nil
}

func (f *fakeDomains) GetDomain(_ context.Context, name string) (*domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.advance(lastSegment(name))
}

// advance is one read of row id: the controller moves a custom host on.
func (f *fakeDomains) advance(id string) (*domain, error) {
	d, ok := f.rows[id]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", id)
	}
	if !d.custom() {
		return copyDomain(d), nil
	}
	f.reads[id]++
	n := f.reads[id]
	switch {
	case d.State == domainDeleting:
		if n >= f.goneAfter {
			delete(f.rows, id)
			return nil, status.Errorf(codes.NotFound, "%s not found", id)
		}
	case n >= f.recordsAfter && len(d.DNSRecords) == 0:
		d.DNSRecords = []domainRecord{
			{Name: d.Hostname, Type: "CNAME", Value: "acme.id.authwise.com", Purpose: "traffic"},
			{Name: "_acme-challenge." + d.Hostname, Type: "CNAME", Value: "abc.authorize.certificatemanager.goog", Purpose: "challenge"},
		}
	case f.fail != "" && n >= f.activeAfter:
		d.State, d.FailureCode, d.FailureMessage = domainFailed, f.fail, "a CAA record at acme.com excludes the certificate authority"
	case n >= f.activeAfter && d.State != domainActive:
		exp := time.Date(2027, 1, 19, 0, 0, 0, 0, time.UTC)
		d.State, d.CertificateExpiresAt = domainActive, &exp
		if f.holdProvisioning {
			d.State = domainProvisioning
		}
	}
	return copyDomain(d), nil
}

func (f *fakeDomains) FindDomain(_ context.Context, parent, hostname string) (*domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.rows {
		if d.Hostname == hostname && strings.HasPrefix(d.Name, parent+"/") {
			return copyDomain(d), nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "no domain %s under %s", hostname, parent)
}

func (f *fakeDomains) UpdateDomainLabels(_ context.Context, name string, labels map[string]string) (*domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.rows[lastSegment(name)]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", name)
	}
	d.Labels = maps.Clone(labels)
	return copyDomain(d), nil
}

func (f *fakeDomains) DeleteDomain(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := lastSegment(name)
	d, ok := f.rows[id]
	if !ok {
		return status.Errorf(codes.NotFound, "%s not found", name)
	}
	if !d.custom() {
		delete(f.rows, id)
		return nil
	}
	d.State = domainDeleting
	f.reads[id] = 0
	return nil
}

// issuerDomainName is a custom row's name through the alias RPCs.
func issuerDomainName(d *domain) *domain {
	tenant := strings.TrimSuffix(d.Name, "/domains/"+lastSegment(d.Name))
	d.Name = fmt.Sprintf("%s/issuers/%s/issuer-domains/%s", tenant, d.IssuerID, lastSegment(d.Name))
	return d
}

func (f *fakeDomains) CreateIssuerDomain(ctx context.Context, parent, hostname string) (*domain, error) {
	tenant, issuer, _ := strings.Cut(parent, "/issuers/")
	d, err := f.CreateDomain(ctx, tenant, &domain{Hostname: hostname, IssuerID: issuer})
	if err != nil {
		return nil, err
	}
	return issuerDomainName(d), nil
}

func (f *fakeDomains) GetIssuerDomain(ctx context.Context, name string) (*domain, error) {
	d, err := f.GetDomain(ctx, name)
	if err != nil {
		return nil, err
	}
	if !d.custom() {
		return nil, status.Errorf(codes.NotFound, "%s not found", name)
	}
	return issuerDomainName(d), nil
}

func (f *fakeDomains) DeleteIssuerDomain(ctx context.Context, name string) error {
	return f.DeleteDomain(ctx, name)
}

func (f *fakeDomains) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// domainTestProvider serves the domain resources, their activations and the
// deprecated aliases over the fake, with the tenant as a provider default as
// the real provider takes it.
type domainTestProvider struct{ fake *fakeDomains }

func (p *domainTestProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = providerTypeName
}

func (p *domainTestProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {
}

func (p *domainTestProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = &tfruntime.ProviderData{
		Clients:  map[string]any{domainsClientKey: p.fake, issuerDomainsClientKey: p.fake},
		Defaults: map[string]string{tenantIDAttribute: "t-1"},
	}
}

func (p *domainTestProvider) Resources(context.Context) []func() resource.Resource {
	poller := domainPoller{poll: time.Millisecond}
	return []func() resource.Resource{
		func() resource.Resource { return &domainResource{domainPoller: poller} },
		func() resource.Resource { return &domainActivationResource{poller, domainActivation} },
		func() resource.Resource { return &issuerDomainResource{domainPoller: poller} },
		func() resource.Resource { return &domainActivationResource{poller, issuerDomainActivation} },
	}
}

func (p *domainTestProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

var _ provider.Provider = &domainTestProvider{}

func (p *domainTestProvider) factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		providerTypeName: providerserver.NewProtocol6WithError(p),
	}
}

const (
	domainAddr           = "authwise_domain.login"
	domainActivationAddr = "authwise_domain_activation.login"
	nativeDomainAddr     = "authwise_domain.native"
)

// customDomainConfig is the documented one-apply pattern over
// authwise_domain, as issuerDomainConfig is over the deprecated alias.
const customDomainConfig = `
resource "authwise_domain" "login" {
  issuer_id = "i-1"
  hostname  = "login.acme.com"
}

locals {
  records = { for r in authwise_domain.login.dns_records : r.purpose => r }
}

resource "terraform_data" "challenge" {
  input = {
    name    = local.records["challenge"].name
    content = local.records["challenge"].value
  }
}

resource "authwise_domain_activation" "login" {
  domain     = authwise_domain.login.name
  depends_on = [terraform_data.challenge]
}

resource "terraform_data" "traffic" {
  input = {
    name    = local.records["traffic"].name
    content = local.records["traffic"].value
    after   = authwise_domain_activation.login.state
  }
}
`

func nativeDomainConfig(labels string) string {
	return fmt.Sprintf(`
resource "authwise_domain" "native" {
  hostname = "acme.id.authwise.test"
  labels   = %s
}
`, labels)
}

func TestDomainNativeLifecycle(t *testing.T) {

	fake := newFakeDomains()
	p := &domainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		CheckDestroy: func(*terraform.State) error {
			if n := fake.count(); n != 0 {
				return fmt.Errorf("destroy returned with %d domains still on the server", n)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: nativeDomainConfig(`{ env = "test" }`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("name"), knownvalue.StringExact("tenants/t-1/domains/d-1")),
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("domain_id"), knownvalue.StringExact("d-1")),
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("hostname"), knownvalue.StringExact("acme.id.authwise.test")),
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("issuer_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("state"), knownvalue.StringExact("")),
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("dns_records"), knownvalue.ListSizeExact(0)),
				},
			},
			{
				// labels change in place.
				Config: nativeDomainConfig(`{ env = "prod" }`),
				ConfigPlanChecks: tfresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(nativeDomainAddr, plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("labels").AtMapKey("env"), knownvalue.StringExact("prod")),
					statecheck.ExpectKnownValue(nativeDomainAddr, tfjsonpath.New("domain_id"), knownvalue.StringExact("d-1")),
				},
			},
		},
	})
}

func TestDomainCustomLifecycle(t *testing.T) {

	fake := newFakeDomains()
	p := &domainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		CheckDestroy: func(*terraform.State) error {
			// Destroy waits out the two-phase delete.
			if n := fake.count(); n != 0 {
				return fmt.Errorf("destroy returned with %d domains still on the server", n)
			}
			return nil
		},
		Steps: []tfresource.TestStep{{
			Config: customDomainConfig,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("name"), knownvalue.StringExact("tenants/t-1/domains/d-1")),
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("domain_id"), knownvalue.StringExact("d-1")),
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("issuer_id"), knownvalue.StringExact("i-1")),
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("dns_records"), knownvalue.ListSizeExact(2)),
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("traffic_target"), knownvalue.StringExact("acme.id.authwise.test")),
				statecheck.ExpectKnownValue(domainActivationAddr, tfjsonpath.New("state"), knownvalue.StringExact(domainActive)),
				statecheck.ExpectKnownValue(domainActivationAddr, tfjsonpath.New("hostname"), knownvalue.StringExact("login.acme.com")),
				statecheck.ExpectKnownValue(domainActivationAddr, tfjsonpath.New("certificate_expires_at"), knownvalue.StringExact("2027-01-19T00:00:00Z")),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("after"), knownvalue.StringExact(domainActive)),
			},
		}},
	})
}

// TestDomainImportPlansNoChange imports by name (#43), and by the
// hostname-keyed name of 0.18 and earlier, which Read resolves.
func TestDomainImportPlansNoChange(t *testing.T) {

	config := nativeDomainConfig(`{ env = "test" }`)

	for importID, desc := range map[string]string{
		"tenants/t-1/domains/d-1":                   "by AWID",
		"tenants/t-1/domains/acme.id.authwise.test": "by hostname",
	} {
		t.Run(desc, func(t *testing.T) {
			p := &domainTestProvider{fake: newFakeDomains()}
			tfresource.UnitTest(t, tfresource.TestCase{
				ProtoV6ProviderFactories: p.factories(),
				Steps: []tfresource.TestStep{
					{Config: config},
					{
						ResourceName:    nativeDomainAddr,
						ImportState:     true,
						ImportStateKind: tfresource.ImportBlockWithID,
						ImportStateId:   importID,
						Config:          config,
					},
				},
			})
		})
	}
}

func TestDomainActivationImportPlansNoChange(t *testing.T) {

	p := &domainTestProvider{fake: newFakeDomains()}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{
			{Config: customDomainConfig},
			{
				ResourceName:    domainActivationAddr,
				ImportState:     true,
				ImportStateKind: tfresource.ImportBlockWithID,
				ImportStateId:   "tenants/t-1/domains/d-1",
				Config:          customDomainConfig,
			},
		},
	})
}

// TestDomainActivationRefusesNative: a native host has no lifecycle, so
// waiting for it would never end.
func TestDomainActivationRefusesNative(t *testing.T) {

	p := &domainTestProvider{fake: newFakeDomains()}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config: nativeDomainConfig(`{}`) + `
resource "authwise_domain_activation" "native" {
  domain = authwise_domain.native.name
}
`,
			ExpectError: regexp.MustCompile(`not a custom host`),
		}},
	})
}

func TestDomainHostnameValidated(t *testing.T) {

	p := &domainTestProvider{fake: newFakeDomains()}

	for config, want := range map[string]string{
		`resource "authwise_domain" "x" {
  issuer_id = "i-1"
  hostname  = "login.acme.com:8443"
}`: `invalid custom hostname`,
		`resource "authwise_domain" "x" {
  hostname = "Acme.id.authwise.com"
}`: `invalid hostname`,
		`resource "authwise_domain" "x" {
  issuer_id = "tenants/t-1/issuers/i-1"
  hostname  = "login.acme.com"
}`: `authwise_issuer.<name>.issuer_id`,
	} {
		tfresource.UnitTest(t, tfresource.TestCase{
			ProtoV6ProviderFactories: p.factories(),
			Steps: []tfresource.TestStep{{
				Config:      config,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(regexp.QuoteMeta(want)),
			}},
		})
	}

	// A native host may carry a port (kit#336).
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config: `resource "authwise_domain" "x" {
  hostname = "localhost:8080"
}`,
		}},
	})
}

// TestDomainMovedFromIssuerDomain: moved blocks carry the deprecated
// resources' state to authwise_domain and authwise_domain_activation with
// nothing to change, on the server or in the DNS records.
func TestDomainMovedFromIssuerDomain(t *testing.T) {

	fake := newFakeDomains()
	p := &domainTestProvider{fake: fake}

	moved := customDomainConfig + `
moved {
  from = authwise_issuer_domain.login
  to   = authwise_domain.login
}

moved {
  from = authwise_issuer_domain_activation.login
  to   = authwise_domain_activation.login
}
`

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{
			{Config: issuerDomainConfig},
			{
				Config: moved,
				ConfigPlanChecks: tfresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(domainAddr, plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction(domainActivationAddr, plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("terraform_data.challenge", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("terraform_data.traffic", plancheck.ResourceActionNoop),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("name"), knownvalue.StringExact("tenants/t-1/domains/d-1")),
					statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("issuer_id"), knownvalue.StringExact("i-1")),
					statecheck.ExpectKnownValue(domainActivationAddr, tfjsonpath.New("domain"), knownvalue.StringExact("tenants/t-1/domains/d-1")),
				},
			},
		},
	})
}

// TestDomainUpgradeFromHostnameKeyed: 0.18's state, keyed by hostname,
// upgrades to the hostname-keyed name and the hostname, which the refresh
// resolves (TestDomainImportPlansNoChange, by hostname).
func TestDomainUpgradeFromHostnameKeyed(t *testing.T) {

	ctx := t.Context()
	r := &domainResource{}
	upgrader := r.UpgradeState(ctx)[0]

	priorType := upgrader.PriorSchema.Type().TerraformType(ctx)
	prior := tftypes.NewValue(priorType, map[string]tftypes.Value{
		"name":      tftypes.NewValue(tftypes.String, "tenants/t-1/domains/acme.id.authwise.com"),
		"domain_id": tftypes.NewValue(tftypes.String, "acme.id.authwise.com"),
		"tenant_id": tftypes.NewValue(tftypes.String, "t-1"),
		"labels":    tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{}),
		"config":    tftypes.NewValue(tftypes.String, "{}"),
	})

	target := domainSchema(ctx)
	req := resource.UpgradeStateRequest{State: &tfsdk.State{Schema: *upgrader.PriorSchema, Raw: prior}}
	resp := &resource.UpgradeStateResponse{State: tfsdk.State{Schema: target, Raw: tftypes.NewValue(target.Type().TerraformType(ctx), nil)}}
	upgrader.StateUpgrader(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}

	var got domainModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.Name.ValueString() != "tenants/t-1/domains/acme.id.authwise.com" || got.Hostname.ValueString() != "acme.id.authwise.com" || got.TenantID.ValueString() != "t-1" {
		t.Fatalf("got %+v", got)
	}
}
