package acctest_test

import (
	"fmt"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"google.golang.org/protobuf/proto"
)

// TestAccRealmAuthenticationPolicy_SkipIfDeviceTrusted pins tf#87: the
// requirement's skip_if_device_trusted is unset, true or false (kit#1026,
// apis v0.90.0), and on a realm's floor an explicit false vetoes "Remember
// this device" for every rule. So an attribute the configuration omits must
// reach kit absent, never false; true and false must round-trip; and state
// must keep the three apart so each plans clean.
func TestAccRealmAuthenticationPolicy_SkipIfDeviceTrusted(t *testing.T) {

	h := newHarness(t)
	const addr = "authwise_realm_authentication_policy.p"

	// floorSkip is the floor's attribute line, or nothing when omitted;
	// minFactors varies an unrelated field.
	config := func(floorSkip string, minFactors int) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "r" {
  display_name = "Employees"
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
    mode        = "ANY_FACTOR"
    min_factors = %d
    %s
  }
}
`, minFactors, floorSkip)
	}

	// floorOnServer is the floor's field as kit holds it: nil when absent.
	floorOnServer := func() *bool {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		return proto.Clone(onlyRealm(t, h.fake.realms)).(*corepb.Realm).
			GetConfig().GetAuthentication().GetFloor().SkipIfDeviceTrusted
	}
	want := func(v *bool) resource.TestCheckFunc {
		return checkServer(func() error {
			got := floorOnServer()
			switch {
			case v == nil && got != nil:
				return fmt.Errorf("floor.skip_if_device_trusted reached kit as %t; an omitted attribute must stay absent", *got)
			case v != nil && got == nil:
				return fmt.Errorf("floor.skip_if_device_trusted is absent on the server; want %t", *v)
			case v != nil && *got != *v:
				return fmt.Errorf("floor.skip_if_device_trusted = %t on the server; want %t", *got, *v)
			}
			return nil
		})
	}
	// The rule's own true is never disturbed by the floor's changes.
	ruleKept := resource.TestCheckResourceAttr(addr, "rules.0.require.skip_if_device_trusted", "true")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Omitted: absent on the server, null in state.
				Config: config("", 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(nil),
					resource.TestCheckNoResourceAttr(addr, "floor.skip_if_device_trusted"),
					ruleKept,
				),
			},
			{
				// An unrelated update writes no false for it.
				Config: config("", 2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					want(nil),
					resource.TestCheckResourceAttr(addr, "floor.min_factors", "2"),
					resource.TestCheckNoResourceAttr(addr, "floor.skip_if_device_trusted"),
					ruleKept,
				),
			},
			{
				Config: config("skip_if_device_trusted = true", 2),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(proto.Bool(true)),
					resource.TestCheckResourceAttr(addr, "floor.skip_if_device_trusted", "true"),
				),
			},
			{
				// An explicit false is the veto, and is sent as such.
				Config: config("skip_if_device_trusted = false", 2),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(proto.Bool(false)),
					resource.TestCheckResourceAttr(addr, "floor.skip_if_device_trusted", "false"),
					ruleKept,
				),
			},
			{
				// From true, omitting it clears it on the server.
				Config: config("skip_if_device_trusted = true", 2),
				Check:  want(proto.Bool(true)),
			},
			{
				Config: config("", 2),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(nil),
					resource.TestCheckNoResourceAttr(addr, "floor.skip_if_device_trusted"),
				),
			},
		},
	})
}
