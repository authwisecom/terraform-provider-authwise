package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeIssuerDomains plays kit and the domain controller: a created domain
// has no records until reads have passed recordsAfter, and turns ACTIVE
// after activeAfter; a deleted one stays DELETING for goneAfter reads.
// With holdProvisioning it stops at PROVISIONING instead, as a real one
// does until the controller finds the traffic record in public DNS.
type fakeIssuerDomains struct {
	mu      sync.Mutex
	domains map[string]*issuerDomain
	reads   map[string]int
	seq     int

	recordsAfter, activeAfter, goneAfter int
	// fail, when set, is the code a domain fails with instead of issuing.
	fail string
	// holdProvisioning keeps an issued domain at PROVISIONING.
	holdProvisioning bool
}

func newFakeIssuerDomains() *fakeIssuerDomains {
	return &fakeIssuerDomains{domains: map[string]*issuerDomain{}, reads: map[string]int{}, recordsAfter: 2, activeAfter: 4, goneAfter: 2}
}

func (f *fakeIssuerDomains) CreateIssuerDomain(_ context.Context, parent, hostname string) (*issuerDomain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.domains {
		if d.Hostname == hostname {
			return nil, status.Errorf(codes.AlreadyExists, "%s is already claimed", hostname)
		}
	}
	f.seq++
	d := &issuerDomain{Name: fmt.Sprintf("%s/issuer-domains/d-%d", parent, f.seq), Hostname: hostname, State: issuerDomainPendingDNS, TrafficTarget: "acme.id.authwise.test"}
	f.domains[d.Name] = d
	cp := *d
	return &cp, nil
}

func (f *fakeIssuerDomains) GetIssuerDomain(_ context.Context, name string) (*issuerDomain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[name]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", name)
	}
	f.reads[name]++
	n := f.reads[name]
	switch {
	case d.State == issuerDomainDeleting:
		if n >= f.goneAfter {
			delete(f.domains, name)
			return nil, status.Errorf(codes.NotFound, "%s not found", name)
		}
	case n >= f.recordsAfter && len(d.DNSRecords) == 0:
		d.DNSRecords = []issuerDomainRecord{
			{Name: d.Hostname, Type: "CNAME", Value: "acme.id.authwise.com", Purpose: "traffic"},
			{Name: "_acme-challenge." + d.Hostname, Type: "CNAME", Value: "abc.authorize.certificatemanager.goog", Purpose: "challenge"},
		}
	case f.fail != "" && n >= f.activeAfter:
		d.State, d.FailureCode, d.FailureMessage = issuerDomainFailed, f.fail, "a CAA record at acme.com excludes the certificate authority"
	case n >= f.activeAfter && d.State != issuerDomainActive:
		exp := time.Date(2027, 1, 19, 0, 0, 0, 0, time.UTC)
		d.State, d.CertificateExpiresAt = issuerDomainActive, &exp
		if f.holdProvisioning {
			d.State = issuerDomainProvisioning
		}
	}
	cp := *d
	return &cp, nil
}

func (f *fakeIssuerDomains) DeleteIssuerDomain(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[name]
	if !ok {
		return status.Errorf(codes.NotFound, "%s not found", name)
	}
	d.State = issuerDomainDeleting
	f.reads[name] = 0
	return nil
}

func (f *fakeIssuerDomains) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.domains)
}

// issuerDomainTestProvider serves authwise_issuer_domain and its activation over the fake,
// with the tenant as a provider default as the real provider takes it.
type issuerDomainTestProvider struct{ fake *fakeIssuerDomains }

func (p *issuerDomainTestProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = providerTypeName
}

func (p *issuerDomainTestProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {
}

func (p *issuerDomainTestProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = &tfruntime.ProviderData{
		Clients:  map[string]any{issuerDomainsClientKey: p.fake},
		Defaults: map[string]string{tenantIDAttribute: "t-1"},
	}
}

func (p *issuerDomainTestProvider) Resources(context.Context) []func() resource.Resource {
	poller := issuerDomainPoller{poll: time.Millisecond}
	return []func() resource.Resource{
		func() resource.Resource { return &issuerDomainResource{issuerDomainPoller: poller} },
		func() resource.Resource { return &issuerDomainActivationResource{poller} },
	}
}

func (p *issuerDomainTestProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

var _ provider.Provider = &issuerDomainTestProvider{}

func (p *issuerDomainTestProvider) factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		providerTypeName: providerserver.NewProtocol6WithError(p),
	}
}

// issuerDomainConfig is the documented one-apply pattern: the challenge
// record, the activation waiting on it, and the traffic record waiting on
// the activation. terraform_data stands in for the DNS provider's record
// resource; the traffic record carries the activation's state, which shows
// it was created only once the domain routed.
const issuerDomainConfig = `
resource "authwise_issuer_domain" "login" {
  issuer_id = "i-1"
  hostname  = "login.acme.com"
}

locals {
  records = { for r in authwise_issuer_domain.login.dns_records : r.purpose => r }
}

resource "terraform_data" "challenge" {
  input = {
    name    = local.records["challenge"].name
    content = local.records["challenge"].value
  }
}

resource "authwise_issuer_domain_activation" "login" {
  issuer_domain = authwise_issuer_domain.login.name
  depends_on    = [terraform_data.challenge]
}

resource "terraform_data" "traffic" {
  input = {
    name    = local.records["traffic"].name
    content = local.records["traffic"].value
    after   = authwise_issuer_domain_activation.login.state
  }
}
`

const issuerDomainActivationAddr = "authwise_issuer_domain_activation.login"

const issuerDomainAddr = "authwise_issuer_domain.login"

const issuerDomainOnlyConfig = `
resource "authwise_issuer_domain" "login" {
  issuer_id = "i-1"
  hostname  = "login.acme.com"
}
`

func TestIssuerDomainLifecycle(t *testing.T) {

	fake := newFakeIssuerDomains()
	p := &issuerDomainTestProvider{fake: fake}

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
			// One apply: the domain returns with its records, the challenge
			// record takes them, the activation waits for the domain to route, and only
			// then is the traffic record created.
			Config: issuerDomainConfig,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("name"), knownvalue.StringExact("tenants/t-1/issuers/i-1/issuer-domains/d-1")),
				statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("issuer_domain_id"), knownvalue.StringExact("d-1")),
				statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("tenant_id"), knownvalue.StringExact("t-1")),
				statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("dns_records"), knownvalue.ListSizeExact(2)),
				statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("traffic_target"), knownvalue.StringExact("acme.id.authwise.test")),
				statecheck.ExpectKnownValue("terraform_data.challenge",
					tfjsonpath.New("output").AtMapKey("name"), knownvalue.StringExact("_acme-challenge.login.acme.com")),
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("state"), knownvalue.StringExact(issuerDomainActive)),
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("hostname"), knownvalue.StringExact("login.acme.com")),
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("certificate_expires_at"), knownvalue.StringExact("2027-01-19T00:00:00Z")),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("after"), knownvalue.StringExact(issuerDomainActive)),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("content"), knownvalue.StringExact("acme.id.authwise.com")),
			},
		}},
	})
}

// TestIssuerDomainImportPlansNoChange imports by name (#43).
func TestIssuerDomainImportPlansNoChange(t *testing.T) {

	p := &issuerDomainTestProvider{fake: newFakeIssuerDomains()}
	config := issuerDomainOnlyConfig

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{
			{Config: config},
			{
				ResourceName:    issuerDomainAddr,
				ImportState:     true,
				ImportStateKind: tfresource.ImportBlockWithID,
				ImportStateId:   "tenants/t-1/issuers/i-1/issuer-domains/d-1",
				Config:          config,
			},
		},
	})
}

// TestIssuerDomainActivationImportPlansNoChange imports the activation by
// the domain's name, after the domain is ACTIVE.
func TestIssuerDomainActivationImportPlansNoChange(t *testing.T) {

	p := &issuerDomainTestProvider{fake: newFakeIssuerDomains()}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{
			{Config: issuerDomainConfig},
			{
				ResourceName:    issuerDomainActivationAddr,
				ImportState:     true,
				ImportStateKind: tfresource.ImportBlockWithID,
				ImportStateId:   "tenants/t-1/issuers/i-1/issuer-domains/d-1",
				Config:          issuerDomainConfig,
			},
		},
	})
}

// TestIssuerDomainActivationReturnsAtProvisioning: a domain routes from
// PROVISIONING, and reaches ACTIVE only once the controller finds the
// traffic record, so the activation must not wait past PROVISIONING (#46).
func TestIssuerDomainActivationReturnsAtProvisioning(t *testing.T) {

	fake := newFakeIssuerDomains()
	fake.holdProvisioning = true
	p := &issuerDomainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config: issuerDomainConfig,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("state"), knownvalue.StringExact(issuerDomainProvisioning)),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("after"), knownvalue.StringExact(issuerDomainProvisioning)),
			},
		}},
	})
}

// TestIssuerDomainFailedSaysWhy: a FAILED domain ends the activation's wait
// with its reason, and the traffic record is never created.
func TestIssuerDomainFailedSaysWhy(t *testing.T) {

	fake := newFakeIssuerDomains()
	fake.fail = "CAA"
	p := &issuerDomainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config:      issuerDomainConfig,
			ExpectError: regexp.MustCompile(`(?s)login\.acme\.com: a CAA record.*\(CAA\)`),
		}},
	})
}

func TestIssuerDomainHostnameValidated(t *testing.T) {

	p := &issuerDomainTestProvider{fake: newFakeIssuerDomains()}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config: `
resource "authwise_issuer_domain" "login" {
  issuer_id = "i-1"
  hostname  = "https://Login.acme.com/"
}
`,
			ExpectError: regexp.MustCompile(`lower-case fully qualified hostname`),
			PlanOnly:    true,
		}},
	})
}

func TestIssuerDomainDeleteTimesOutWhileDeleting(t *testing.T) {

	fake := newFakeIssuerDomains()
	fake.goneAfter = 1 << 30
	r := issuerDomainPoller{client: fake, poll: time.Millisecond}

	d, err := fake.CreateIssuerDomain(t.Context(), "tenants/t-1/issuers/i-1", "login.acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := fake.DeleteIssuerDomain(t.Context(), d.Name); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	var diags diag.Diagnostics
	got := r.waitFor(ctx, d, routes, "state PROVISIONING or ACTIVE", &diags)
	if !diags.HasError() || got.State != issuerDomainDeleting {
		t.Fatalf("expected a timeout on a DELETING domain, got %v (%s)", diags, got.State)
	}
}

func TestIssuerDomainCreateErrorNamesReason(t *testing.T) {

	st, err := status.New(codes.FailedPrecondition, "plan gate").WithDetails(&errdetails.ErrorInfo{
		Domain: authwiseErrorDomain, Reason: "PLAN_REQUIRED", Metadata: map[string]string{"plan": "BUSINESS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, detail := issuerDomainCreateError(st.Err())
	if summary != "create issuer domain refused: PLAN_REQUIRED" || !strings.Contains(detail, "the Business plan or above") {
		t.Fatalf("got %q / %q", summary, detail)
	}

	st, err = status.New(codes.FailedPrecondition, "no controller").WithDetails(&errdetails.ErrorInfo{Domain: authwiseErrorDomain, Reason: "DOMAINS_UNAVAILABLE"})
	if err != nil {
		t.Fatal(err)
	}
	summary, detail = issuerDomainCreateError(st.Err())
	if summary != "create issuer domain refused: DOMAINS_UNAVAILABLE" || !strings.Contains(detail, "no domain controller") {
		t.Fatalf("got %q / %q", summary, detail)
	}

	summary, _ = issuerDomainCreateError(status.Error(codes.Internal, "boom"))
	if summary != "create issuer domain failed" {
		t.Fatalf("an error without ErrorInfo: got %q", summary)
	}
}
