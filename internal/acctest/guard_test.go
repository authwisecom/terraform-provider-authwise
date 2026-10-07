// Guard (guard-control, apis v0.22.0, #32): a tenant registered with
// Guard, a network in it, a relay, a resource and the nodes serving it, and
// an invite whose code is shown once. guard-control is its own endpoint,
// reached with the same bearer as kit.
package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	guardTenantAddr   = "authwise_guard_tenant.acme"
	guardNetworkAddr  = "authwise_guard_network.office"
	guardRelayAddr    = "authwise_guard_relay.west"
	guardResourceAddr = "authwise_guard_resource.wiki"
	guardNodesAddr    = "authwise_guard_resource_nodes.wiki"
	guardInviteAddr   = "authwise_guard_invite.dana"
)

// guardConfig is a tenant, a network, a relay at priority, a host resource
// and an invite granting it; extra is appended.
func (h *harness) guardConfig(priority int, extra string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_guard_tenant" "acme" {
  guard_tenant_id = "t-1"
  display_name    = "Acme"
}

resource "authwise_guard_network" "office" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  display_name = "Office"
}

resource "authwise_guard_relay" "west" {
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "us-west"
  url          = "wss://relay.example.com:8443/v1/transport"
  region       = "us-west1"
  priority     = %d
}

resource "authwise_guard_resource" "wiki" {
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "wiki"
  kind         = "host"
  address      = "10.0.0.5"
}

resource "authwise_guard_invite" "dana" {
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "for Dana"
  grants       = [authwise_guard_resource.wiki.name]
}

data "authwise_guard_networks" "all" {
  tenant_id  = authwise_guard_tenant.acme.guard_tenant_id
  depends_on = [authwise_guard_network.office]
}
%s`, priority, extra)
}

func TestAccGuard_Lifecycle(t *testing.T) {

	h := newHarness(t)

	// The network's name, once it exists, to seed nodes under.
	network := func() string {
		h.guard.mu.Lock()
		defer h.guard.mu.Unlock()
		return keys(h.guard.networks)[0]
	}

	var code string

	nodes := func(ids ...string) string {
		quoted := make([]string, len(ids))
		for i, id := range ids {
			quoted[i] = fmt.Sprintf("%q", id)
		}
		return fmt.Sprintf(`
resource "authwise_guard_resource_nodes" "wiki" {
  guard_resource = authwise_guard_resource.wiki.name
  nodes          = [%s]
}
`, strings.Join(quoted, ", "))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.guardConfig(10, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardTenantAddr, "name", "tenants/t-1"),
					resource.TestCheckResourceAttr(guardTenantAddr, "status", "ACTIVE"),
					resource.TestMatchResourceAttr(guardNetworkAddr, "name", regexp.MustCompile(`^tenants/t-1/networks/n-[0-9]+$`)),
					resource.TestMatchResourceAttr(guardNetworkAddr, "guard_network_id", regexp.MustCompile(`^n-[0-9]+$`)),
					// Unset, the cidr is the whole mesh range.
					resource.TestCheckResourceAttr(guardNetworkAddr, "cidr", "100.64.0.0/10"),
					resource.TestCheckResourceAttr(guardRelayAddr, "priority", "10"),
					resource.TestCheckNoResourceAttr(guardResourceAddr, "serving_node_ids.#"),
					resource.TestMatchResourceAttr(guardInviteAddr, "code", regexp.MustCompile(`^gi_`)),
					resource.TestMatchResourceAttr(guardInviteAddr, "url", regexp.MustCompile(`^https://join.example.com/i/gi_`)),
					resource.TestCheckResourceAttr(guardInviteAddr, "max_uses", "1"),
					resource.TestCheckResourceAttr(guardInviteAddr, "state", "ACTIVE"),
					resource.TestCheckResourceAttrSet(guardInviteAddr, "expires_at"),
					resource.TestCheckResourceAttrPair(guardInviteAddr, "grants.0", guardResourceAddr, "name"),
					resource.TestCheckResourceAttr("data.authwise_guard_networks.all", "guard_networks.#", "1"),
					resource.TestCheckResourceAttrWith(guardInviteAddr, "code", func(v string) error {
						code = v
						return nil
					}),
					func(*terraform.State) error {
						h.guard.mu.Lock()
						defer h.guard.mu.Unlock()
						for _, a := range h.guard.auths {
							if a != "Bearer acc-test-token" {
								return fmt.Errorf("guard-control saw authorization %q", a)
							}
						}
						if len(h.guard.auths) == 0 {
							return fmt.Errorf("guard-control saw no calls")
						}
						return nil
					},
				),
			},
			{
				// Two nodes join; the resource is served by both, and the
				// relay's priority changes in place. The invite's code
				// survives the refreshes.
				PreConfig: func() {
					h.guard.seedNode(network(), "nd-1")
					h.guard.seedNode(network(), "nd-2")
				},
				Config: h.guardConfig(20, nodes(
					"tenants/t-1/networks/${authwise_guard_network.office.guard_network_id}/nodes/nd-1",
					"tenants/t-1/networks/${authwise_guard_network.office.guard_network_id}/nodes/nd-2")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(guardRelayAddr, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(guardInviteAddr, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardRelayAddr, "priority", "20"),
					resource.TestCheckResourceAttr(guardNodesAddr, "nodes.#", "2"),
					resource.TestCheckResourceAttrWith(guardInviteAddr, "code", equalTo(&code)),
				),
			},
			{
				// One node stops serving it. The resource's own echo of the
				// set is read-only, so it follows without a diff.
				Config: h.guardConfig(20, nodes(
					"tenants/t-1/networks/${authwise_guard_network.office.guard_network_id}/nodes/nd-2")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardNodesAddr, "nodes.#", "1"),
					func(*terraform.State) error {
						h.guard.mu.Lock()
						defer h.guard.mu.Unlock()
						for _, r := range h.guard.resources {
							if got := r.GetServingNodeIds(); len(got) != 1 || got[0] != "nd-2" {
								return fmt.Errorf("serving_node_ids = %v, want [nd-2]", got)
							}
						}
						return nil
					},
				),
			},
			{
				// Spent, the invite reads back SPENT and still plans clean:
				// terraform does not reissue it.
				PreConfig: func() {
					h.guard.mu.Lock()
					name := keys(h.guard.invites)[0]
					h.guard.mu.Unlock()
					h.guard.spend(name)
				},
				Config: h.guardConfig(20, nodes(
					"tenants/t-1/networks/${authwise_guard_network.office.guard_network_id}/nodes/nd-2")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardInviteAddr, "state", "SPENT"),
					resource.TestCheckResourceAttr(guardInviteAddr, "uses", "1"),
					resource.TestCheckResourceAttrWith(guardInviteAddr, "code", equalTo(&code)),
				),
			},
			{
				ResourceName:                         guardTenantAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateIdFunc:                    importByName(guardTenantAddr),
			},
			{
				ResourceName:                         guardNetworkAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"tenant_id"},
				ImportStateIdFunc:                    importByName(guardNetworkAddr),
			},
			{
				ResourceName:                         guardRelayAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"network_id"},
				ImportStateIdFunc:                    importByName(guardRelayAddr),
			},
			{
				ResourceName:                         guardResourceAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"network_id"},
				ImportStateIdFunc:                    importByName(guardResourceAddr),
			},
			{
				// An imported invite has no code: no read returns it.
				ResourceName:                         guardInviteAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"code", "url", "network_id"},
				ImportStateIdFunc:                    importByName(guardInviteAddr),
			},
			{
				// The association goes, then the nodes leave as an admin
				// would remove them: guard-control refuses to delete a
				// network that still has nodes.
				Config: h.guardConfig(20, ""),
				Check: func(*terraform.State) error {
					h.guard.mu.Lock()
					defer h.guard.mu.Unlock()
					clear(h.guard.nodes)
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			h.guard.mu.Lock()
			defer h.guard.mu.Unlock()
			if n := len(h.guard.tenants) + len(h.guard.networks) + len(h.guard.relays) + len(h.guard.resources) + len(h.guard.invites); n != 0 {
				return fmt.Errorf("%d Guard rows remain after destroy", n)
			}
			return nil
		},
	})
}

// A network's cidr cannot change in place: a new one replaces it.
func TestAccGuard_NetworkCIDRReplaces(t *testing.T) {

	h := newHarness(t)

	config := func(cidr string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_guard_tenant" "acme" {
  guard_tenant_id = "t-1"
  display_name    = "Acme"
}

resource "authwise_guard_network" "office" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  display_name = "Office"
  cidr         = %q
}
`, cidr)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("100.80.0.0/16"),
				Check:  resource.TestCheckResourceAttr(guardNetworkAddr, "cidr", "100.80.0.0/16"),
			},
			{
				Config: config("100.81.0.0/16"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(guardNetworkAddr, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.TestCheckResourceAttr(guardNetworkAddr, "cidr", "100.81.0.0/16"),
			},
		},
	})
}

// guard-control's refusals reach the apply.
func TestAccGuard_Refusals(t *testing.T) {

	cases := map[string]struct {
		config string
		want   *regexp.Regexp
	}{
		"resource kind ip": {
			config: `
resource "authwise_guard_resource" "x" {
  network_id = authwise_guard_network.office.guard_network_id
  kind       = "ip"
  address    = "10.0.0.5"
}`,
			want: regexp.MustCompile(`(?s)kind\s+ip\s+is\s+gone`),
		},
		"host inside the mesh": {
			config: `
resource "authwise_guard_resource" "x" {
  network_id = authwise_guard_network.office.guard_network_id
  kind       = "host"
  address    = "100.64.0.9"
}`,
			want: regexp.MustCompile(`(?s)not\s+a\s+routed\s+address\s+outside`),
		},
		"invite too long-lived": {
			config: `
resource "authwise_guard_invite" "x" {
  network_id = authwise_guard_network.office.guard_network_id
  expires_at = "2099-01-01T00:00:00Z"
}`,
			want: regexp.MustCompile(`(?s)at\s+most\s+30\s+days\s+ahead`),
		},
		"node granted itself": {
			config: `
resource "authwise_guard_node" "x" {
  network_id = authwise_guard_network.office.guard_network_id
}

resource "authwise_guard_node_grants" "x" {
  guard_node = authwise_guard_node.x.name
  grants     = [authwise_guard_node.x.name]
}`,
			want: regexp.MustCompile(`(?s)cannot\s+be\s+granted\s+itself`),
		},
		"node address outside the network": {
			config: `
resource "authwise_guard_node" "x" {
  network_id = authwise_guard_network.office.guard_network_id
  address    = "10.1.2.3"
}`,
			want: regexp.MustCompile(`(?s)not\s+inside\s+the\s+network's\s+cidr`),
		},
		"tenant that is not a kit AWID": {
			config: `
resource "authwise_guard_tenant" "x" {
  guard_tenant_id = "acme"
  display_name    = "Acme"
}`,
			want: regexp.MustCompile(`(?s)tenant\s+AWID`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config: h.providerConfig() + `
resource "authwise_guard_tenant" "acme" {
  guard_tenant_id = "t-1"
  display_name    = "Acme"
}

resource "authwise_guard_network" "office" {
  tenant_id = authwise_guard_tenant.acme.guard_tenant_id
}
` + v.config,
					ExpectError: v.want,
				}},
			})
		})
	}
}

// Without guard_endpoint a Guard resource fails at plan, saying what to set.
func TestAccGuard_NotConfigured(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "authwise" {
  endpoint      = %q
  insecure      = true
  token_url     = %q
  client_id     = "acc-client"
  client_secret = "acc-secret"
  tenant_id     = "t-1"
}

resource "authwise_guard_tenant" "acme" {
  guard_tenant_id = "t-1"
  display_name    = "Acme"
}
`, h.grpcAddr, h.tokenServer.URL+"/oauth/token"),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`(?s)guard_endpoint\s+is\s+not\s+configured`),
		}},
	})
}

// Renaming a network whose cidr was left out changes it in place: the
// allocated cidr is not a change.
func TestAccGuard_NetworkRenameKeepsCIDR(t *testing.T) {

	h := newHarness(t)

	config := func(name string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_guard_tenant" "acme" {
  guard_tenant_id = "t-1"
  display_name    = "Acme"
}

resource "authwise_guard_network" "office" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  display_name = %q
}
`, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: config("Office")},
			{
				Config: config("HQ"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(guardNetworkAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(guardNetworkAddr, "cidr", "100.64.0.0/10"),
			},
		},
	})
}
