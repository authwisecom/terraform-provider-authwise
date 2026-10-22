package acctest_test

import (
	"context"
	"fmt"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
		Name:     fmt.Sprintf("%s/issuer-domains/d-%d", in.GetParent(), f.seq),
		Hostname: hostname,
		State:    corepb.IssuerDomainState_ISSUER_DOMAIN_STATE_PENDING_DNS,
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
// identity service (#44): Create returns with the records, wait_for_active
// reads the state through the enum, and destroy waits out DELETING.
func TestAccIssuerDomain_Lifecycle(t *testing.T) {

	h := newHarness(t)
	const addr = "authwise_issuer_domain.login"

	config := func(wait bool) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_issuer_domain" "login" {
  hostname        = "login.acme.com"
  wait_for_active = %t
}
`, wait)
	}

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
				Config: config(false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "tenants/t-1/issuers/i-1/issuer-domains/d-1"),
					resource.TestCheckResourceAttr(addr, "issuer_id", "i-1"),
					resource.TestCheckResourceAttr(addr, "state", "PENDING_DNS"),
					resource.TestCheckResourceAttr(addr, "failure_code", ""),
					resource.TestCheckResourceAttr(addr, "dns_records.#", "2"),
					resource.TestCheckResourceAttr(addr, "dns_records.1.purpose", "challenge"),
					resource.TestCheckResourceAttr(addr, "dns_records.1.name", "_acme-challenge.login.acme.com"),
				),
			},
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "state", "ACTIVE"),
					resource.TestCheckResourceAttr(addr, "certificate_expires_at", "2026-12-14T00:00:00Z"),
				),
			},
			{
				// wait_for_active is configuration only, so the import is
				// checked against the default.
				Config: config(false),
			},
			importPlansNoChange(addr),
		},
	})
}
