package acctest_test

import (
	"context"
	"fmt"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (f *fakeIdentityServer) CreateIssuerDomain(ctx context.Context, in *identitypb.CreateIssuerDomainRequest) (*corepb.IssuerDomain, error) {
	f.recordAuth(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()

	hostname := in.GetIssuerDomain().GetHostname()
	for _, d := range f.issuerDomains {
		if d.GetHostname() == hostname {
			st, _ := status.New(codes.AlreadyExists, hostname+" is claimed").
				WithDetails(&errdetails.ErrorInfo{Domain: "authwise.io", Reason: "HOSTNAME_CLAIMED"})
			return nil, st.Err()
		}
	}

	f.seq++
	d := &corepb.IssuerDomain{
		Name:          fmt.Sprintf("%s/issuer-domains/d-%d", in.GetParent(), f.seq),
		Hostname:      hostname,
		State:         corepb.IssuerDomainState_ISSUER_DOMAIN_STATE_PENDING_DNS,
		TrafficTarget: "acme.id.authwise.test",
	}
	f.issuerDomains[d.GetName()] = d
	return proto.Clone(d).(*corepb.IssuerDomain), nil
}

func (f *fakeIdentityServer) GetIssuerDomain(ctx context.Context, in *identitypb.GetIssuerDomainRequest) (*corepb.IssuerDomain, error) {
	f.recordAuth(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.issuerDomains[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", in.GetName())
	}
	f.issuerDomainReads[d.GetName()]++
	n := f.issuerDomainReads[d.GetName()]

	switch {
	case d.GetState() == corepb.IssuerDomainState_ISSUER_DOMAIN_STATE_DELETING:
		if n >= 2 {
			delete(f.issuerDomains, d.GetName())
			return nil, status.Errorf(codes.NotFound, "%s not found", in.GetName())
		}
	case n == 1:
		d.DnsRecords = []*corepb.IssuerDomainDnsRecord{
			{Name: d.GetHostname(), Type: "CNAME", Value: "acme.id.authwise.com", Purpose: "traffic"},
			{Name: "_acme-challenge." + d.GetHostname(), Type: "CNAME", Value: "abc.authorize.certificatemanager.goog", Purpose: "challenge"},
		}
	case n >= 2:
		d.State = corepb.IssuerDomainState_ISSUER_DOMAIN_STATE_ACTIVE
		d.CertificateExpiresAt = timestamppb.New(certNotBefore.AddDate(0, 3, 0))
	}
	return proto.Clone(d).(*corepb.IssuerDomain), nil
}

func (f *fakeIdentityServer) DeleteIssuerDomain(ctx context.Context, in *identitypb.DeleteIssuerDomainRequest) (*emptypb.Empty, error) {
	f.recordAuth(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.issuerDomains[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", in.GetName())
	}
	d.State = corepb.IssuerDomainState_ISSUER_DOMAIN_STATE_DELETING
	f.issuerDomainReads[d.GetName()] = 0
	return &emptypb.Empty{}, nil
}

// TestAccIssuerDomain_Lifecycle drives authwise_issuer_domain over the
// identity service (#44): Create returns with the records, the activation
// reads the state through the enum, and destroy waits out DELETING.
func TestAccIssuerDomain_Lifecycle(t *testing.T) {

	h := newHarness(t)
	const addr = "authwise_issuer_domain.login"
	const activation = "authwise_issuer_domain_activation.login"

	domain := h.providerConfig() + `
resource "authwise_issuer_domain" "login" {
  hostname = "login.acme.com"
}
`
	activated := domain + `
resource "authwise_issuer_domain_activation" "login" {
  issuer_domain = authwise_issuer_domain.login.name
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		CheckDestroy: func(*terraform.State) error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if n := len(h.fake.issuerDomains); n != 0 {
				return fmt.Errorf("destroy returned with %d issuer domains on the server", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: domain,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "tenants/t-1/issuers/i-1/issuer-domains/d-1"),
					resource.TestCheckResourceAttr(addr, "issuer_id", "i-1"),
					resource.TestCheckResourceAttr(addr, "state", "PENDING_DNS"),
					resource.TestCheckResourceAttr(addr, "failure_code", ""),
					resource.TestCheckResourceAttr(addr, "dns_records.#", "2"),
					resource.TestCheckResourceAttr(addr, "traffic_target", "acme.id.authwise.test"),
					resource.TestCheckResourceAttr(addr, "dns_records.1.purpose", "challenge"),
					resource.TestCheckResourceAttr(addr, "dns_records.1.name", "_acme-challenge.login.acme.com"),
				),
			},
			{
				Config: activated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(activation, "state", "ACTIVE"),
					resource.TestCheckResourceAttr(activation, "hostname", "login.acme.com"),
					resource.TestCheckResourceAttr(activation, "certificate_expires_at", "2026-12-14T00:00:00Z"),
				),
			},
			importPlansNoChange(addr),
			{
				// The activation imports by the domain's name, its only argument.
				ResourceName:    activation,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources[activation].Primary.Attributes["issuer_domain"], nil
				},
			},
		},
	})
}

// TestAccIssuerDomain_UpgradeFrom017 applies with the released 0.17.1, whose
// schema has wait_for_active, then plans with this build: the framework
// ignores the attribute the schema no longer has, and nothing changes.
func TestAccIssuerDomain_UpgradeFrom017(t *testing.T) {

	h := newHarness(t)
	config := func(extra string) string {
		return h.providerConfig() + `
resource "authwise_issuer_domain" "login" {
  hostname = "login.acme.com"
` + extra + `}
`
	}

	resource.Test(t, resource.TestCase{
		Steps: []resource.TestStep{
			{
				ExternalProviders: map[string]resource.ExternalProvider{
					"authwise": {Source: "authwisecom/authwise", VersionConstraint: "0.17.1"},
				},
				Config: config("  wait_for_active = true\n"),
			},
			{
				ProtoV6ProviderFactories: protoFactories(),
				Config:                   config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}
