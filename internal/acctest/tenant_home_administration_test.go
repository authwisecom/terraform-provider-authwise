// Home administration (kit#916, tf#70): EnableHomeAdministration records
// the administration entry on the tenant; there is no disable on the
// public service, so destroy forgets.
package acctest_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// EnableHomeAdministration is kit's HA0: idempotent at the same issuer,
// the issuer must be the tenant's, and it needs the owner's
// tenancy.administration.enable (denyAdministration stands in for a caller
// without it).
func (f *fakeTenancyServer) EnableHomeAdministration(_ context.Context, in *tenancypb.EnableHomeAdministrationRequest) (*corepb.Tenant, error) {

	issuer := in.GetName() + "/issuers/" + in.GetIssuerId()
	f.identity.mu.Lock()
	_, known := f.identity.issuers[issuer]
	f.identity.mu.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denyAdministration {
		return nil, status.Error(codes.PermissionDenied, "tenancy.administration.enable is required at the tenant")
	}
	t, ok := f.tenants[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", in.GetName())
	}
	if !known {
		return nil, status.Errorf(codes.InvalidArgument, "issuer_id: no issuer %q in %s", in.GetIssuerId(), in.GetName())
	}
	if e, ok := t.GetProducts()["administration"]; ok {
		if e.GetIssuerId() != in.GetIssuerId() {
			return nil, status.Errorf(codes.FailedPrecondition, "home administration is enabled at issuer %s", e.GetIssuerId())
		}
		return proto.Clone(t).(*corepb.Tenant), nil
	}
	if t.Products == nil {
		t.Products = map[string]*corepb.TenantProduct{}
	}
	t.Products["administration"] = &corepb.TenantProduct{IssuerId: in.GetIssuerId(), AudienceId: f.nextID("a"), ConsoleClientId: f.nextID("c")}
	return proto.Clone(t).(*corepb.Tenant), nil
}

func homeAdministrationConfig(h *harness, withConfig bool) string {
	cfg := h.providerConfig() + `
resource "authwise_domain" "home" {
  hostname = "acme.id.authwise.test"
}

resource "authwise_issuer" "home" {
  domain_id = authwise_domain.home.domain_id
}

resource "authwise_tenant_home_administration" "this" {
  issuer_id = authwise_issuer.home.issuer_id
}
`
	if withConfig {
		cfg += `
resource "authwise_tenant_config" "this" {
  config     = jsonencode({})
  depends_on = [authwise_tenant_home_administration.this]
}
`
	}
	return cfg
}

func TestAccTenantHomeAdministration(t *testing.T) {
	h := newHarness(t)

	const addr = "authwise_tenant_home_administration.this"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: homeAdministrationConfig(h, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "tenants/t-1"),
					resource.TestCheckResourceAttrPair(addr, "issuer_id", "authwise_issuer.home", "issuer_id"),
					resource.TestMatchResourceAttr(addr, "audience_id", regexp.MustCompile(`^a-`)),
					resource.TestMatchResourceAttr(addr, "console_client_id", regexp.MustCompile(`^c-`)),
					resource.TestCheckResourceAttrPair("authwise_tenant_config.this", "administration.audience_id", addr, "audience_id"),
					resource.TestCheckResourceAttrPair("authwise_tenant_config.this", "administration.issuer_id", addr, "issuer_id"),
				),
			},
			{
				Config: homeAdministrationConfig(h, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        "t-1",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
			{
				// An operator disabled it: the refresh finds it gone and
				// the plan enables it again.
				PreConfig: func() {
					h.tenancy.editTenant("tenants/t-1", func(t *corepb.Tenant) { delete(t.GetProducts(), "administration") })
				},
				Config: homeAdministrationConfig(h, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
			{
				// Destroy forgets: the entry stays on the tenant.
				Config: h.providerConfig() + `
resource "authwise_domain" "home" {
  hostname = "acme.id.authwise.test"
}

resource "authwise_issuer" "home" {
  domain_id = authwise_domain.home.domain_id
}
`,
				Check: func(*terraform.State) error {
					if _, ok := h.tenancy.tenant("tenants/t-1").GetProducts()["administration"]; !ok {
						return fmt.Errorf("the administration entry went with the resource")
					}
					return nil
				},
			},
		},
	})
}

// TestAccTenantHomeAdministration_NotOwner: a caller without the owner's
// permission is refused, and told why.
func TestAccTenantHomeAdministration_NotOwner(t *testing.T) {
	h := newHarness(t)
	h.tenancy.mu.Lock()
	h.tenancy.denyAdministration = true
	h.tenancy.mu.Unlock()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config:      homeAdministrationConfig(h, false),
			ExpectError: regexp.MustCompile(`(?s)only\s+tenancy\.owner\s+holds`),
		}},
	})
}
