// The endpoint's life after creation (apis v0.10.0): :check dials it, and
// kit refuses to delete it — or the certificate or issuer it uses — while
// something still names it.
package acctest_test

import (
	"fmt"
	"net"
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// identityClient dials the harness's stub the way a consumer of the
// published client would.
func identityClient(t *testing.T, h *harness) identitypb.AuthwiseIdentityServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(h.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return identitypb.NewAuthwiseIdentityServiceClient(conn)
}

// TestAccEndpoint_Check creates two endpoints — one on a listener this test
// holds open, one on a port nothing listens on — and checks both through
// the client. An unreachable endpoint is a report, not an error.
func TestAccEndpoint_Check(t *testing.T) {

	h := newHarness(t)
	client := identityClient(t, h)

	up, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = up.Close() })
	go func() {
		for {
			c, err := up.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	down, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	downAddr := down.Addr().String()
	_ = down.Close()

	config := h.providerConfig() + fmt.Sprintf(`
resource "authwise_issuer" "internal" {
  domain_name = "auth.example.com"
}

resource "authwise_endpoint" "up" {
  endpoint_type = "GRPC"
  address       = %q
  insecure      = true
  auth = jsonencode({
    kitToken = { issuer = authwise_issuer.internal.name, audience = "https://up.example.com" }
  })
}

resource "authwise_endpoint" "down" {
  endpoint_type = "GRPC"
  address       = %q
  insecure      = true
}
`, up.Addr().String(), downAddr)

	check := func(resourceName string, assert func(*identitypb.CheckEndpointResponse) error) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			name := s.RootModule().Resources[resourceName].Primary.Attributes["name"]
			resp, err := client.CheckEndpoint(t.Context(), &identitypb.CheckEndpointRequest{Name: name})
			if err != nil {
				return fmt.Errorf("check %s: %w", name, err)
			}
			return assert(resp)
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					check("authwise_endpoint.up", func(r *identitypb.CheckEndpointResponse) error {
						if !r.GetReachable() || r.GetAuthScheme() != "kit_token" || !r.GetCredentialResolved() {
							return fmt.Errorf("up: %v", r)
						}
						return nil
					}),
					check("authwise_endpoint.down", func(r *identitypb.CheckEndpointResponse) error {
						if r.GetReachable() || r.GetError() == "" || r.GetAuthScheme() != "none" {
							return fmt.Errorf("down: %v", r)
						}
						return nil
					}),
				),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccEndpoint_ReferenceOrdersDestroy: an audience naming the endpoint
// by reference inside its config JSON depends on it, so destroy removes the
// audience first and kit never refuses the endpoint's delete.
func TestAccEndpoint_ReferenceOrdersDestroy(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_endpoint" "flow" {
  address = "https://flow.example.com"
}

resource "authwise_audience" "api" {
  display_name = "https://api.example.com"
  config = jsonencode({
    flowIntegrationConfig = { endpointName = authwise_endpoint.flow.name }
  })
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: checkServer(func() error {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					for name := range h.fake.endpoints {
						if refs := h.fake.endpointReferrersLocked(name); len(refs) != 1 || refs[0].GetReferrerType() != "audience" {
							return fmt.Errorf("referrers = %v", refs)
						}
					}
					return nil
				}),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if n := len(h.fake.endpoints) + len(h.fake.audiences); n != 0 {
				return fmt.Errorf("%d objects left after destroy", n)
			}
			return nil
		},
	})
}

// TestAccEndpoint_DeleteRefusedWhileNamed: a holder terraform cannot see —
// an audience naming the endpoint as a literal string, written outside
// this configuration — makes kit refuse the delete, and the error names it.
// That is the case the docs send to depends_on.
func TestAccEndpoint_DeleteRefusedWhileNamed(t *testing.T) {

	h := newHarness(t)

	endpoint := h.providerConfig() + `
resource "authwise_endpoint" "flow" {
  address = "https://flow.example.com"
}
`
	var holder string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: endpoint,
				Check: func(s *terraform.State) error {
					name := s.RootModule().Resources["authwise_endpoint.flow"].Primary.Attributes["name"]
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					holder = "tenants/t-1/issuers/i-1/audiences/a-outside"
					h.fake.audiences[holder] = &corepb.Audience{
						Name:   holder,
						Config: &corepb.AudienceConfig{FlowIntegrationConfig: &corepb.IntegrationConfig{EndpointName: name}},
					}
					return nil
				},
			},
			{
				Config:      h.providerConfig(),
				ExpectError: regexp.MustCompile(`in use by audience`),
			},
			{
				// With the holder gone, the delete goes through.
				PreConfig: func() {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					delete(h.fake.audiences, holder)
				},
				Config: h.providerConfig(),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}
