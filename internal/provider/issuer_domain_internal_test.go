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
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeIssuerDomains plays kit and the domain controller: a created domain
// has no records until reads have passed recordsAfter, and turns ACTIVE
// after activeAfter; a deleted one stays DELETING for goneAfter reads.
type fakeIssuerDomains struct {
	mu      sync.Mutex
	domains map[string]*issuerDomain
	reads   map[string]int
	seq     int

	recordsAfter, activeAfter, goneAfter int
	// fail, when set, is the code a domain fails with instead of issuing.
	fail string
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
	d := &issuerDomain{Name: fmt.Sprintf("%s/domains/d-%d", parent, f.seq), Hostname: hostname, State: issuerDomainPendingDNS}
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

// issuerDomainTestProvider serves authwise_issuer_domain alone over the fake,
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
	return []func() resource.Resource{func() resource.Resource {
		return &issuerDomainResource{poll: time.Millisecond}
	}}
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

// issuerDomainConfig is the documented pattern: DNS records keyed on purpose,
// which is known at plan, with the apply-time values in the map values.
// terraform_data stands in for the DNS provider's record resource.
func issuerDomainConfig(waitForActive bool) string {
	return fmt.Sprintf(`
resource "authwise_issuer_domain" "login" {
  issuer_id       = "i-1"
  hostname        = "login.acme.com"
  wait_for_active = %t
}

resource "terraform_data" "record" {
  for_each = toset(["traffic", "challenge"])
  input = {
    name    = one([for r in authwise_issuer_domain.login.dns_records : r.name if r.purpose == each.key])
    type    = one([for r in authwise_issuer_domain.login.dns_records : r.type if r.purpose == each.key])
    content = one([for r in authwise_issuer_domain.login.dns_records : r.value if r.purpose == each.key])
  }
}
`, waitForActive)
}

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

	// Destroy waits out the two-phase delete, so the fake is empty after.
	defer func() {
		if n := fake.count(); n != 0 {
			t.Errorf("destroy returned with %d domains still on the server", n)
		}
	}()

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{
			{
				// Create returns with the records, and the DNS resources take
				// them in the same apply.
				Config: issuerDomainConfig(false),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("name"), knownvalue.StringExact("tenants/t-1/issuers/i-1/domains/d-1")),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("issuer_domain_id"), knownvalue.StringExact("d-1")),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("tenant_id"), knownvalue.StringExact("t-1")),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("state"), knownvalue.StringExact(issuerDomainPendingDNS)),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("dns_records"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("certificate_expires_at"), knownvalue.StringExact("")),
					statecheck.ExpectKnownValue(`terraform_data.record["challenge"]`,
						tfjsonpath.New("output").AtMapKey("name"), knownvalue.StringExact("_acme-challenge.login.acme.com")),
					statecheck.ExpectKnownValue(`terraform_data.record["traffic"]`,
						tfjsonpath.New("output").AtMapKey("content"), knownvalue.StringExact("acme.id.authwise.com")),
				},
			},
			{
				// Setting wait_for_active is an in-place update that waits.
				Config: issuerDomainConfig(true),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("state"), knownvalue.StringExact(issuerDomainActive)),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("certificate_expires_at"), knownvalue.StringExact("2027-01-19T00:00:00Z")),
					statecheck.ExpectKnownValue(issuerDomainAddr, tfjsonpath.New("name"), knownvalue.StringExact("tenants/t-1/issuers/i-1/domains/d-1")),
				},
			},
			{
				// The record resources go before the case ends: the test
				// framework's teardown cannot shim for_each instances.
				Config: issuerDomainOnlyConfig,
			},
		},
	})
}

// TestIssuerDomainImportPlansNoChange imports by name (#43). The domain is
// alone here: the test framework's import step cannot shim for_each
// instances either.
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
				ImportStateId:   "tenants/t-1/issuers/i-1/domains/d-1",
				Config:          config,
			},
		},
	})
}

func TestIssuerDomainFailedSaysWhy(t *testing.T) {

	fake := newFakeIssuerDomains()
	fake.fail = "CAA"
	p := &issuerDomainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config:      issuerDomainConfig(true),
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
	r := &issuerDomainResource{client: fake, poll: time.Millisecond}

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
	got := r.waitFor(ctx, d, isActive, "state ACTIVE", &diags)
	if !diags.HasError() || got.State != issuerDomainDeleting {
		t.Fatalf("expected a timeout on a DELETING domain, got %v (%s)", diags, got.State)
	}
}

func TestIssuerDomainCreateErrorNamesReason(t *testing.T) {

	st, err := status.New(codes.FailedPrecondition, "plan gate").WithDetails(&errdetails.ErrorInfo{
		Domain: authwiseErrorDomain, Reason: "PLAN_REQUIRED", Metadata: map[string]string{"plan": "Business"},
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, detail := issuerDomainCreateError(st.Err())
	if summary != "create issuer domain refused: PLAN_REQUIRED" || !strings.Contains(detail, "the Business plan or above") {
		t.Fatalf("got %q / %q", summary, detail)
	}

	summary, _ = issuerDomainCreateError(status.Error(codes.Internal, "boom"))
	if summary != "create issuer domain failed" {
		t.Fatalf("an error without ErrorInfo: got %q", summary)
	}
}
