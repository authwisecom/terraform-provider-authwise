// Parent identifiers (#43): tenant_id, realm_id and the rest read back from
// the name, so an import has them whether the configuration sets one or
// inherits the provider default, and the first plan after it is empty.
// Changing a configured parent still replaces the resource.
package acctest_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const (
	parentRealmAddr  = "authwise_realm.r"
	parentFactorAddr = "authwise_factor.totp"
)

// parentConfig is a realm that inherits the provider's tenant_id, a factor
// that configures its realm_id, and a Guard network that configures its
// tenant_id. realmName is the realm's display name.
func (h *harness) parentConfig(realmName string) string {
	return h.providerConfig() + guardTenantHCL + fmt.Sprintf(`
resource "authwise_realm" "r" {
  display_name = %q
}

data "authwise_factor_totp" "totp" {}

resource "authwise_factor" "totp" {
  realm_id     = authwise_realm.r.realm_id
  display_name = "Authenticator app"
  factor_type  = "totp"
  config       = data.authwise_factor_totp.totp.any
}

resource "authwise_guard_network" "office" {
  tenant_id    = data.authwise_guard_tenant.acme.guard_tenant_id
  display_name = "Office"
}
`, realmName)
}

// importPlansNoChange imports addr with an import block into the state
// without it, and fails unless the plan is a no-op import.
func importPlansNoChange(addr string) resource.TestStep {
	return resource.TestStep{
		ResourceName:      addr,
		ImportState:       true,
		ImportStateKind:   resource.ImportBlockWithID,
		ImportStateIdFunc: importByName(addr),
	}
}

func TestAccParentIdentifiers_ImportPlansNoChange(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.parentConfig("Employees"),
				Check: resource.ComposeAggregateTestCheckFunc(
					// An inherited parent shows the provider default.
					resource.TestCheckResourceAttr(parentRealmAddr, "tenant_id", "t-1"),
					resource.TestCheckResourceAttrPair(parentFactorAddr, "realm_id", parentRealmAddr, "realm_id"),
					resource.TestCheckResourceAttr(guardNetworkAddr, "tenant_id", "gt-acme"),
				),
			},
			importPlansNoChange(parentRealmAddr),
			importPlansNoChange(parentFactorAddr),
			importPlansNoChange(guardNetworkAddr),
			{
				// Another change plans the inherited parent unknown before
				// the plan modifiers run; it must not replace the realm.
				Config: h.parentConfig("Staff"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(parentRealmAddr, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(parentFactorAddr, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.TestCheckResourceAttr(parentRealmAddr, "tenant_id", "t-1"),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

func TestAccParentIdentifiers_ChangingAConfiguredParentReplaces(t *testing.T) {

	h := newHarness(t)

	config := func(realm string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "a" {
  display_name = "A"
}

resource "authwise_realm" "b" {
  display_name = "B"
}

data "authwise_factor_totp" "totp" {}

resource "authwise_factor" "totp" {
  realm_id     = authwise_realm.%s.realm_id
  display_name = "Authenticator app"
  factor_type  = "totp"
  config       = data.authwise_factor_totp.totp.any
}
`, realm)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("a"),
				Check:  resource.TestCheckResourceAttrPair(parentFactorAddr, "realm_id", "authwise_realm.a", "realm_id"),
			},
			{
				Config: config("b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(parentFactorAddr, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttrPair(parentFactorAddr, "realm_id", "authwise_realm.b", "realm_id"),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}
