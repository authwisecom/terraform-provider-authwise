// tf#78: a JSON config the server changed out of band reads back in the
// form the practitioner's own value had. A realm whose live config was
// changed to the same content as a new config, written with jsonencode or
// with authwise_realm_config, then plans empty. Before, state took
// protojson's field order while both sort their keys, so the plan showed a
// whitespace-only update and deferred everything that read the realm.
package acctest_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const jsonStyleRealmAddr = "authwise_realm.signup"

const jsonStyleSecret = `
resource "authwise_secret" "turnstile" {
  display_name = "Turnstile secret"
  payload_wo   = "turnstile-secret-material"
}
`

// realmBotConfig is a realm whose bot check has site key siteKey and the
// key fields in field order apart from the sorted ones, with the config
// given by jsonencode or by the data source.
func (h *harness) realmBotConfig(siteKey string, dataSource bool) string {

	if dataSource {
		return h.providerConfig() + jsonStyleSecret + fmt.Sprintf(`
data "authwise_realm_config" "signup" {
  recovery = { self_service_reset = true, support_contact = "help@example.com" }
  bot_protection = {
    mode                     = "ALWAYS"
    provider                 = "TURNSTILE"
    site_key                 = %q
    secret_ref               = { name = authwise_secret.turnstile.name }
    refuse_disposable_signup = true
  }
}

resource "authwise_realm" "signup" {
  display_name = "Customers"
  config       = data.authwise_realm_config.signup.json
}
`, siteKey)
	}

	return h.providerConfig() + jsonStyleSecret + fmt.Sprintf(`
resource "authwise_realm" "signup" {
  display_name = "Customers"
  config = jsonencode({
    recovery = { selfServiceReset = true, supportContact = "help@example.com" }
    botProtection = {
      mode                   = "ALWAYS"
      provider               = "TURNSTILE"
      siteKey                = %q
      secretRef              = { name = authwise_secret.turnstile.name }
      refuseDisposableSignup = true
    }
  })
}
`, siteKey)
}

func TestAccJSONStyle_OutOfBand(t *testing.T) {

	for name, dataSource := range map[string]bool{"jsonencode": false, "data source": true} {
		t.Run(name, func(t *testing.T) {

			h := newHarness(t)
			var realm string

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{
						Config: h.realmBotConfig("0x4AAAAAAA", dataSource),
						Check:  resource.TestCheckResourceAttrWith(jsonStyleRealmAddr, "name", func(v string) error { realm = v; return nil }),
					},
					{
						// The site key changes in the console; the code is
						// changed to match. Nothing is left to apply.
						PreConfig: func() {
							h.fake.mu.Lock()
							defer h.fake.mu.Unlock()
							h.fake.realms[realm].GetConfig().GetBotProtection().SiteKey = "0x4BBBBBBB"
						},
						Config: h.realmBotConfig("0x4BBBBBBB", dataSource),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
				},
			})
		})
	}
}
