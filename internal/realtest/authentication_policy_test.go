package realtest_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestReal_RealmAuthenticationPolicySkipIfDeviceTrusted is tf#87 against
// kit: the floor's skip_if_device_trusted is unset, true or false, and each
// must survive kit's own storage so that every step plans clean. A kit
// without kit#1026 stores a plain bool and reads an explicit false back as
// unset, so on it the false step never plans clean (versions.env).
func TestReal_RealmAuthenticationPolicySkipIfDeviceTrusted(t *testing.T) {
	const addr = "authwise_realm_authentication_policy.p"

	cfg := func(floorSkip string) string {
		return providerHCL(t) + fmt.Sprintf(`
resource "authwise_realm" "r" {
  display_name = "tf87"
}

resource "authwise_realm_authentication_policy" "p" {
  realm = authwise_realm.r.name
  rules = [
    {
      name = "default"
      require = {
        mode                   = "ANY_FACTOR"
        skip_if_device_trusted = true
      }
    },
  ]
  floor = {
    mode = "ANY_FACTOR"
    %s
  }
}
`, floorSkip)
	}
	empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{
				Config:           cfg(""),
				ConfigPlanChecks: empty,
				Check:            resource.TestCheckNoResourceAttr(addr, "floor.skip_if_device_trusted"),
			},
			{
				Config:           cfg("skip_if_device_trusted = true"),
				ConfigPlanChecks: empty,
				Check:            resource.TestCheckResourceAttr(addr, "floor.skip_if_device_trusted", "true"),
			},
			{
				Config:           cfg("skip_if_device_trusted = false"),
				ConfigPlanChecks: empty,
				Check:            resource.TestCheckResourceAttr(addr, "floor.skip_if_device_trusted", "false"),
			},
			{
				Config:           cfg(""),
				ConfigPlanChecks: empty,
				Check:            resource.TestCheckNoResourceAttr(addr, "floor.skip_if_device_trusted"),
			},
		},
	})
}
