// Estate relay pools (tf#66, guard-control#81, apis v0.59.0): pools and
// their relays, a tenant's access, a network on a pool by slug or by the
// tenant's default, and relay credentials issued once and kept.
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
	guardPoolCentralAddr = "authwise_guard_relay_pool.central"
	guardPoolEastAddr    = "authwise_guard_relay_pool.east"
	guardPoolRelayAddr   = "authwise_guard_pool_relay.c1"
	guardPoolCredAddr    = "authwise_guard_relay_credential.c1"
	guardOwnCredAddr     = "authwise_guard_relay_credential.west"
	guardPoolAccessAddr  = "authwise_guard_relay_pool_access.acme"
	guardPooledNetAddr   = "authwise_guard_network.pooled"
	guardDefaultNetAddr  = "authwise_guard_network.defaulted"
)

// relayPoolsConfig is two estate pools, central the estate default; a
// relay in pool and its credential at rotation; t-1 allowed east; a
// network on east by slug and one on the default; a network relay with a
// credential of its own.
func (h *harness) relayPoolsConfig(pool, rotation string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_guard_relay_pool" "central" {
  slug         = "us-central"
  display_name = "US central"
  region       = "us-central1"
  select       = "nearest"
  default      = true
}

resource "authwise_guard_relay_pool" "east" {
  slug         = "us-east"
  display_name = "US east"
  region       = "us-east1"
}

resource "authwise_guard_pool_relay" "c1" {
  pool         = authwise_guard_relay_pool.%s.name
  display_name = "c1"
  url          = "wss://c1.relay.example.com/v1/transport"
  region       = "us-central1"
}

resource "authwise_guard_relay_credential" "c1" {
  relay    = authwise_guard_pool_relay.c1.name
  rotation = %q
}

resource "authwise_guard_relay_pool_access" "acme" {
  tenant_id     = "t-1"
  allowed_pools = [authwise_guard_relay_pool.east.name]
}

resource "authwise_guard_network" "pooled" {
  tenant_id    = "t-1"
  display_name = "Pooled"
  relay_pool   = authwise_guard_relay_pool.east.name
  depends_on   = [authwise_guard_relay_pool_access.acme]
}

resource "authwise_guard_network" "defaulted" {
  tenant_id    = "t-1"
  display_name = "Defaulted"
  depends_on   = [authwise_guard_relay_pool.central]
}

resource "authwise_guard_relay" "west" {
  tenant_id    = "t-1"
  network_id   = authwise_guard_network.pooled.guard_network_id
  display_name = "own"
  url          = "wss://own.example.com/v1/transport"
}

resource "authwise_guard_relay_credential" "west" {
  relay = authwise_guard_relay.west.name
}

data "authwise_guard_allowed_relay_pools" "acme" {
  tenant_id  = "t-1"
  depends_on = [authwise_guard_relay_pool_access.acme]
}

data "authwise_guard_relay_pools" "all" {
  depends_on = [authwise_guard_relay_pool.central, authwise_guard_relay_pool.east]
}
`, pool, rotation)
}

func TestAccGuardRelayPools(t *testing.T) {

	h := newHarness(t)

	var firstCred, relayName, relayID string

	// heldCredential checks the state's credential is the one
	// guard-control holds for the relay, and notes it.
	heldCredential := func(addr string, note *string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			rs := s.RootModule().Resources[addr].Primary.Attributes
			held := h.guard.credential(rs["relay"])
			if held == "" || rs["credential"] != held {
				return fmt.Errorf("%s holds %q, guard-control %q", addr, rs["credential"], held)
			}
			if note != nil {
				*note = held
			}
			return nil
		}
	}

	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.relayPoolsConfig("central", "1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(guardPoolCentralAddr, "name", regexp.MustCompile(`^relay-pools/rp-`)),
					resource.TestCheckResourceAttr(guardPoolCentralAddr, "select", "nearest"),
					resource.TestCheckResourceAttr(guardPoolEastAddr, "select", "served-order"),
					resource.TestMatchResourceAttr(guardPoolRelayAddr, "name", regexp.MustCompile(`^pool-relays/rl-`)),
					resource.TestCheckResourceAttrPair(guardPoolRelayAddr, "pool", guardPoolCentralAddr, "name"),
					resource.TestMatchResourceAttr(guardPoolCredAddr, "credential", regexp.MustCompile(`^grk_`)),
					heldCredential(guardPoolCredAddr, &firstCred),
					heldCredential(guardOwnCredAddr, nil),
					resource.TestCheckResourceAttrPair(guardPoolAccessAddr, "allowed_pools.0", guardPoolEastAddr, "name"),
					resource.TestCheckResourceAttr(guardPoolAccessAddr, "default_pool", ""),
					// The default is stored as the pool's name.
					resource.TestCheckResourceAttrPair(guardPooledNetAddr, "relay_pool", guardPoolEastAddr, "name"),
					resource.TestCheckResourceAttrPair(guardDefaultNetAddr, "relay_pool", guardPoolCentralAddr, "name"),
					resource.TestCheckResourceAttr(guardPooledNetAddr, "pool_fallback", "false"),
					resource.TestCheckResourceAttr(guardPooledNetAddr, "pool_killed", "false"),
					resource.TestCheckResourceAttr("data.authwise_guard_allowed_relay_pools.acme", "relay_pools.#", "2"),
					resource.TestCheckResourceAttr("data.authwise_guard_allowed_relay_pools.acme", "relay_pools.0.slug", "us-central"),
					resource.TestCheckResourceAttr("data.authwise_guard_allowed_relay_pools.acme", "relay_pools.0.default", "true"),
					resource.TestCheckResourceAttr("data.authwise_guard_relay_pools.all", "guard_relay_pools.#", "2"),
					func(s *terraform.State) error {
						relayName = s.RootModule().Resources[guardPoolRelayAddr].Primary.Attributes["name"]
						relayID = s.RootModule().Resources[guardPoolRelayAddr].Primary.Attributes["guard_pool_relay_id"]
						return nil
					},
				),
			},
			{
				// What moves with the relays rather than a write reads
				// back at the refresh, without a plan.
				Config:           h.relayPoolsConfig("central", "1"),
				ConfigPlanChecks: empty,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardPoolCentralAddr, "relay_count", "1"),
					resource.TestCheckResourceAttr("authwise_guard_relay.west", "has_credential", "true"),
					resource.TestCheckResourceAttr(guardPoolRelayAddr, "has_credential", "true"),
				),
			},
			{
				// Moving the relay is an update; a new rotation replaces
				// the credential, and only it.
				Config: h.relayPoolsConfig("east", "2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(guardPoolRelayAddr, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(guardPoolCredAddr, plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction(guardOwnCredAddr, plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(guardPoolRelayAddr, "pool", guardPoolEastAddr, "name"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[guardPoolRelayAddr].Primary.Attributes["name"]; got != relayName {
							return fmt.Errorf("the relay was replaced: %s, was %s", got, relayName)
						}
						return nil
					},
					heldCredential(guardPoolCredAddr, nil),
					func(s *terraform.State) error {
						if s.RootModule().Resources[guardPoolCredAddr].Primary.Attributes["credential"] == firstCred {
							return fmt.Errorf("the credential was not rotated")
						}
						return nil
					},
				),
			},
			{Config: h.relayPoolsConfig("east", "2"), ConfigPlanChecks: empty},
			{
				// An operator revokes it: the refresh drops the
				// credential, and the apply issues a new one.
				PreConfig: func() { h.guard.revoke(relayName) },
				Config:    h.relayPoolsConfig("east", "2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(guardPoolCredAddr, plancheck.ResourceActionCreate),
				}},
				Check: heldCredential(guardPoolCredAddr, nil),
			},
			{
				ResourceName:                         guardPoolRelayAddr,
				ImportState:                          true,
				ImportStateIdFunc:                    func(*terraform.State) (string, error) { return relayID, nil },
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// The state's is from before the credential was issued
				// again: it follows the credential, not a write.
				ImportStateVerifyIgnore: []string{"has_credential"},
			},
			{
				ResourceName:                         guardPoolCentralAddr,
				ImportState:                          true,
				ImportStateIdFunc:                    attrOf(guardPoolCentralAddr, "guard_relay_pool_id"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// relay_count follows the relays, not a write.
				ImportStateVerifyIgnore: []string{"relay_count"},
			},
			{
				ResourceName:                         guardPoolAccessAddr,
				ImportState:                          true,
				ImportStateId:                        "t-1",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "tenant_id",
			},
		},
		// Destroy revokes nothing, but deleting a relay does.
		CheckDestroy: func(*terraform.State) error {
			if c := h.guard.credential(relayName); c != "" {
				return fmt.Errorf("pool relay %s still holds a credential", relayName)
			}
			return nil
		},
	})
}

// attrOf is an ImportStateIdFunc reading one attribute of addr.
func attrOf(addr, attribute string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return "", fmt.Errorf("%s not in state", addr)
		}
		return rs.Primary.Attributes[attribute], nil
	}
}

// guard-control's refusals reach the plan's apply in its own words.
func TestAccGuardRelayPools_Refusals(t *testing.T) {

	h := newHarness(t)

	cases := map[string]struct {
		hcl  string
		want string
	}{
		"pool not allowed": {
			hcl: `
resource "authwise_guard_relay_pool" "private" {
  slug = "private"
}

resource "authwise_guard_network" "n" {
  tenant_id    = "t-1"
  display_name = "n"
  relay_pool   = authwise_guard_relay_pool.private.name
}
`,
			want: `(?s)relay\s+pool\s+relay-pools/rp-\S+\s+is\s+not\s+allowed`,
		},
		"pool by slug": {
			hcl: `
resource "authwise_guard_relay_pool" "central" {
  slug    = "us-central"
  default = true
}

resource "authwise_guard_network" "n" {
  tenant_id    = "t-1"
  display_name = "n"
  relay_pool   = "us-central"
  depends_on   = [authwise_guard_relay_pool.central]
}
`,
			want: `(?s)relay\s+pool\s+us-central\s+is\s+not\s+allowed`,
		},
		"pool fallback without a relay": {
			hcl: `
resource "authwise_guard_network" "n" {
  tenant_id     = "t-1"
  display_name  = "n"
  pool_fallback = true
}
`,
			want: `(?s)pool_fallback\s+needs\s+a\s+relay`,
		},
		"bad select": {
			hcl: `
resource "authwise_guard_relay_pool" "p" {
  slug   = "p"
  select = "random"
}
`,
			want: `(?s)select\s+"random"`,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config:      h.providerConfig() + c.hcl,
					ExpectError: regexp.MustCompile(c.want),
				}},
			})
		})
	}
}
