// TI7: a trusted issuer whose keys a node of the network reports
// (key_source_node, guard-control v0.32.0). The issuer starts with no keys,
// a report supplies them with nothing for Terraform to do, a report that
// would remove every key is held and read back, the other key sources are
// refused beside it, and a guard-control that does not serve it is named.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const keySourceIssuerAddr = "authwise_guard_trusted_issuer.ops"

func (h *harness) keySourceConfig(keys string) string {
	return h.providerConfig() + guardTenantHCL + fmt.Sprintf(`
resource "authwise_guard_network" "office" {
  tenant_id    = local.guard_tenant_id
  display_name = "Office"
}

resource "authwise_guard_node" "reporter" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "reporter"
  public_key   = "cmVwb3J0ZXItcHVibGljLWtleQ=="
}

resource "authwise_guard_trusted_issuer" "ops" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "ops"
  issuer       = "https://id.ops.internal"
  %s
}
`, keys)
}

func TestAccGuardKeySource(t *testing.T) {

	h := newHarness(t)
	source := h.keySourceConfig("key_source_node = authwise_guard_node.reporter.name")
	var issuer string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: source,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(keySourceIssuerAddr, "key_source_node", "authwise_guard_node.reporter", "name"),
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "key_ids.#", "0"),
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "held_key_ids.#", "0"),
					resource.TestCheckResourceAttrWith(keySourceIssuerAddr, "name", func(v string) error { issuer = v; return nil }),
				),
			},
			{
				// The node's first report supplies the keys.
				PreConfig: func() { h.guard.reportKeys(issuer, "k1", "k2") },
				Config:    source,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "key_ids.#", "2"),
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "key_ids.0", "k1"),
				),
			},
			{
				// A report that would remove every key is held, not applied.
				PreConfig: func() { h.guard.reportKeys(issuer, "k9") },
				Config:    source,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "key_ids.#", "2"),
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "held_key_ids.#", "1"),
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "held_key_ids.0", "k9"),
					resource.TestCheckResourceAttrSet(keySourceIssuerAddr, "keys_held_at"),
				),
			},
			{
				// Removing the source is made in place; an upload takes over.
				Config: h.keySourceConfig(`jwks = "{\"keys\":[]}"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(keySourceIssuerAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(keySourceIssuerAddr, "key_source_node"),
					resource.TestCheckResourceAttr(keySourceIssuerAddr, "held_key_ids.#", "0"),
				),
			},
		},
	})
}

func TestAccGuardKeySource_Refusals(t *testing.T) {

	h := newHarness(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      h.keySourceConfig("key_source_node = authwise_guard_node.reporter.name\n  jwks = \"{}\""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`jwks and key_source_node are exclusive`),
			},
			{
				Config:      h.keySourceConfig("key_source_node = authwise_guard_node.reporter.name\n  discover = true"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`discover and key_source_node are exclusive`),
			},
		},
	})
}

// TestAccGuardKeySource_EarlierGuardControl: a guard-control that does not
// serve key sources answers Unimplemented; the provider names the cause.
func TestAccGuardKeySource_EarlierGuardControl(t *testing.T) {

	h := newHarness(t)
	h.guard.k8s.noKeySource = true
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      h.keySourceConfig("key_source_node = authwise_guard_node.reporter.name"),
				ExpectError: regexp.MustCompile(`(?s)key_source_node needs guard-control v0\.32\.0`),
			},
		},
	})
}
