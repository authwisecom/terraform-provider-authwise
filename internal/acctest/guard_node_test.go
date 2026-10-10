// Guard nodes and their grants (#32 stage 2): a server registered ahead of
// time, its one-time auth code, enrolment writing its key and endpoint
// without a diff, and what it may reach as an authoritative set.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	guardDBAddr     = "authwise_guard_node.db"
	guardWebAddr    = "authwise_guard_node.web"
	guardGrantsAddr = "authwise_guard_node_grants.web"
)

// guardNodeConfig is a network with a host resource, a pending server node
// "db", and, with extra, more.
func (h *harness) guardNodeConfig(dbName, extra string) string {
	return h.providerConfig() + fmt.Sprintf(`
`+guardTenantHCL+`
resource "authwise_guard_network" "office" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  display_name = "Office"
  cidr         = "100.96.0.0/16"
}

resource "authwise_guard_resource" "wiki" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "wiki"
  kind         = "host"
  address      = "10.0.0.5"
}

resource "authwise_guard_node" "db" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = %q
}
%s`, dbName, extra)
}

func TestAccGuardNode_RegisterEnrolGrant(t *testing.T) {

	h := newHarness(t)

	var authCode, dbName string

	web := func(address string) string {
		return fmt.Sprintf(`
resource "authwise_guard_node" "web" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "web"
  public_key   = "d2ViLXB1YmxpYy1rZXk="
  address      = %q
}

resource "authwise_guard_node_grants" "web" {
  guard_node = authwise_guard_node.web.name
  grants = [
    authwise_guard_node.db.name,
    authwise_guard_resource.wiki.name,
  ]
}
`, address)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Without a key the node is PENDING, with an address
				// allocated and an auth code shown once.
				Config: h.guardNodeConfig("db", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardDBAddr, "state", "PENDING"),
					resource.TestMatchResourceAttr(guardDBAddr, "auth_code", regexp.MustCompile(`^ga_`)),
					resource.TestCheckResourceAttrSet(guardDBAddr, "auth_code_expires_at"),
					resource.TestCheckResourceAttr(guardDBAddr, "address", "100.96.0.1"),
					resource.TestCheckResourceAttr(guardDBAddr, "allowed_ips.0", "100.96.0.1/32"),
					resource.TestMatchResourceAttr(guardDBAddr, "guard_node_id", regexp.MustCompile(`^nd-`)),
					resource.TestCheckNoResourceAttr(guardDBAddr, "public_key"),
					resource.TestCheckResourceAttrWith(guardDBAddr, "auth_code", func(v string) error { authCode = v; return nil }),
					resource.TestCheckResourceAttrWith(guardDBAddr, "name", func(v string) error { dbName = v; return nil }),
				),
			},
			{
				// The server enrols with the code: its key and endpoint are
				// the device's, and read back without a diff.
				PreConfig: func() { h.guard.enrol(dbName, "ZGItcHVibGljLWtleQ==", "203.0.113.5:51820") },
				Config:    h.guardNodeConfig("db", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardDBAddr, "state", "ACTIVE"),
					resource.TestCheckResourceAttr(guardDBAddr, "public_key", "ZGItcHVibGljLWtleQ=="),
					resource.TestCheckResourceAttr(guardDBAddr, "endpoint", "203.0.113.5:51820"),
					resource.TestCheckNoResourceAttr(guardDBAddr, "auth_code_expires_at"),
					resource.TestCheckResourceAttrWith(guardDBAddr, "auth_code", equalTo(&authCode)),
				),
			},
			{
				// A node created with its key is ACTIVE at once and has no
				// auth code; a pinned address is kept. It may reach db and
				// the wiki.
				Config: h.guardNodeConfig("db", web("100.96.0.50")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardWebAddr, "state", "ACTIVE"),
					resource.TestCheckNoResourceAttr(guardWebAddr, "auth_code"),
					resource.TestCheckResourceAttr(guardWebAddr, "address", "100.96.0.50"),
					resource.TestCheckResourceAttr(guardGrantsAddr, "grants.#", "2"),
					func(*terraform.State) error {
						h.guard.mu.Lock()
						defer h.guard.mu.Unlock()
						for name, targets := range h.guard.grants {
							if name != dbName && len(targets) != 2 {
								return fmt.Errorf("%s grants %v, want db and the wiki", name, targets)
							}
						}
						return nil
					},
				),
			},
			{
				// db is now a server (web is granted it); renaming it is in
				// place and keeps the code.
				Config: h.guardNodeConfig("db-primary", web("100.96.0.50")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(guardDBAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardDBAddr, "display_name", "db-primary"),
					resource.TestCheckResourceAttr(guardDBAddr, "roles.0", "server"),
					resource.TestCheckResourceAttrWith(guardDBAddr, "auth_code", equalTo(&authCode)),
				),
			},
			{
				// A grant made out of band is removed: the set is
				// authoritative.
				PreConfig: func() {
					laptop := h.guard.seedNode(networkOf(dbName), "nd-laptop")
					h.guard.mu.Lock()
					defer h.guard.mu.Unlock()
					for name := range h.guard.grants {
						if name != dbName {
							h.guard.grants[name] = append(h.guard.grants[name], laptop)
						}
					}
				},
				Config: h.guardNodeConfig("db-primary", web("100.96.0.50")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(guardGrantsAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(guardGrantsAddr, "grants.#", "2"),
			},
			{
				// The address cannot change in place.
				Config: h.guardNodeConfig("db-primary", web("100.96.0.60")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(guardWebAddr, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardWebAddr, "address", "100.96.0.60"),
					// The seeded laptop leaves, as its owner would remove it.
					func(*terraform.State) error {
						h.guard.mu.Lock()
						defer h.guard.mu.Unlock()
						delete(h.guard.nodes, networkOf(dbName)+"/nodes/nd-laptop")
						return nil
					},
				),
			},
			{
				// An imported node has no auth code: no read returns it.
				ResourceName:                         guardDBAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"auth_code", "tenant_id", "network_id"},
				ImportStateIdFunc:                    importByName(guardDBAddr),
			},
			{
				ResourceName:                         guardGrantsAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "guard_node",
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources[guardWebAddr].Primary.Attributes["name"], nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			h.guard.mu.Lock()
			defer h.guard.mu.Unlock()
			if n := len(h.guard.nodes) + len(h.guard.networks) + len(h.guard.tenants); n != 0 {
				return fmt.Errorf("%d Guard rows remain after destroy", n)
			}
			return nil
		},
	})
}
