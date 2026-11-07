// Password rule sets (kit#890, tf#59; apis v0.51.0): a realm names the
// catalog set it follows, and its password_validators are extras on top.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// passwordRulesRealm is a realm whose provisioning is the HCL given.
func passwordRulesRealm(h *harness, provisioning string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "r" {
  display_name = "Customers"
  config = provider::authwise::realm_config({
    provisioning = {
%s
    }
  })
}
`, provisioning)
}

const digitExtra = `
      password_validators = [
        {
          name   = "digit"
          type   = "regexp"
          config = jsonencode({ pattern = "[0-9]", no_match_message = "Must contain a digit" })
        },
      ]`

// TestAccRealm_PasswordRuleSet: the baseline (left out, so absent on the
// server and no diff after apply), custom with an extra, and back.
func TestAccRealm_PasswordRuleSet(t *testing.T) {

	h := newHarness(t)

	ruleSet := func(want string, validators int) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			p := onlyRealm(t, h.fake.realms).GetConfig().GetProvisioning()
			if got := p.GetPasswordRuleSet(); got != want {
				return fmt.Errorf("password_rule_set = %q, want %q", got, want)
			}
			if got := len(p.GetPasswordValidators()); got != validators {
				return fmt.Errorf("%d password_validators, want %d", got, validators)
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: passwordRulesRealm(h, digitExtra),
				Check:  ruleSet("", 1),
			},
			{
				// The empty default plans nothing.
				Config: passwordRulesRealm(h, digitExtra),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: passwordRulesRealm(h, `      password_rule_set = "custom"`+digitExtra),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_realm.r", plancheck.ResourceActionUpdate),
					},
				},
				Check: ruleSet("custom", 1),
			},
			{
				Config: passwordRulesRealm(h, `      password_rule_set = "authwise-baseline"`),
				Check:  ruleSet("authwise-baseline", 0),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccRealm_PasswordRuleSetUnknown: kit refuses a set its build does
// not ship, and its message is the apply error as it stands. The name is
// not checked at plan: kit's admission owns the catalog.
func TestAccRealm_PasswordRuleSetUnknown(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: passwordRulesRealm(h, `      password_rule_set = "nist"`),
			ExpectError: regexp.MustCompile(`(?s)password_rule_set\s+"nist"\s+is\s+not\s+a\s+set\s+this\s+build\s+ships\s+` +
				`\(authwise-baseline,\s+authwise-strict,\s+custom\);\s+omit\s+it\s+for\s+authwise-baseline\.`),
		}},
		CheckDestroy: noneLeft(h),
	})
}
