package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("state"), knownvalue.StringExact(domainActive)),
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("hostname"), knownvalue.StringExact("login.acme.com")),
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("certificate_expires_at"), knownvalue.StringExact("2027-01-19T00:00:00Z")),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("after"), knownvalue.StringExact(domainActive)),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("content"), knownvalue.StringExact("acme.id.authwise.com")),
			},
		}},
	})
}

// TestIssuerDomainImportPlansNoChange imports by name (#43).
func TestIssuerDomainImportPlansNoChange(t *testing.T) {

	p := &domainTestProvider{fake: newFakeDomains()}
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

	p := &domainTestProvider{fake: newFakeDomains()}

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

	fake := newFakeDomains()
	fake.holdProvisioning = true
	p := &domainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config: issuerDomainConfig,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(issuerDomainActivationAddr, tfjsonpath.New("state"), knownvalue.StringExact(domainProvisioning)),
				statecheck.ExpectKnownValue("terraform_data.traffic",
					tfjsonpath.New("output").AtMapKey("after"), knownvalue.StringExact(domainProvisioning)),
			},
		}},
	})
}

// TestIssuerDomainFailedSaysWhy: a FAILED domain ends the activation's wait
// with its reason, and the traffic record is never created.
func TestIssuerDomainFailedSaysWhy(t *testing.T) {

	fake := newFakeDomains()
	fake.fail = "CAA"
	p := &domainTestProvider{fake: fake}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: p.factories(),
		Steps: []tfresource.TestStep{{
			Config:      issuerDomainConfig,
			ExpectError: regexp.MustCompile(`(?s)login\.acme\.com: a CAA record.*\(CAA\)`),
		}},
	})
}

func TestIssuerDomainHostnameValidated(t *testing.T) {

	p := &domainTestProvider{fake: newFakeDomains()}

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

	fake := newFakeDomains()
	fake.goneAfter = 1 << 30
	r := domainPoller{get: fake.GetIssuerDomain, poll: time.Millisecond}

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
	if !diags.HasError() || got.State != domainDeleting {
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
	summary, detail := domainCreateError(st.Err())
	if summary != "create domain refused: PLAN_REQUIRED" || !strings.Contains(detail, "the Business plan or above") {
		t.Fatalf("got %q / %q", summary, detail)
	}

	st, err = status.New(codes.FailedPrecondition, "no controller").WithDetails(&errdetails.ErrorInfo{Domain: authwiseErrorDomain, Reason: "DOMAINS_UNAVAILABLE"})
	if err != nil {
		t.Fatal(err)
	}
	summary, detail = domainCreateError(st.Err())
	if summary != "create domain refused: DOMAINS_UNAVAILABLE" || !strings.Contains(detail, "no domain controller") {
		t.Fatalf("got %q / %q", summary, detail)
	}

	summary, _ = domainCreateError(status.Error(codes.Internal, "boom"))
	if summary != "create domain failed" {
		t.Fatalf("an error without ErrorInfo: got %q", summary)
	}
}
