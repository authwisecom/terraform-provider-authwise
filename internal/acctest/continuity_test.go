// Returning-user continuity (kit#878, tf#58; apis v0.50.0): an issuer's
// remember_identifiers and persistent_session, and a realm session's idle
// bound.
package acctest_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccIssuerConfig_RememberIdentifiersPresence: remember_identifiers is
// tri-state, and absent means on for an identifier-first issuer. Unset must
// stay absent rather than become false, or every existing issuer would
// turn it off; false must be sent as false.
func TestAccIssuerConfig_RememberIdentifiersPresence(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
data "authwise_issuer_config" "unset" {}

data "authwise_issuer_config" "off" {
  remember_identifiers = false
}

output "unset" {
  value = data.authwise_issuer_config.unset.json
}

output "off" {
  value = data.authwise_issuer_config.off.json
}

output "fn_unset" {
  value = provider::authwise::issuer_config({ persistent_session = true })
}

output "fn_off" {
  value = provider::authwise::issuer_config({ remember_identifiers = false })
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("unset", "{}"),
				resource.TestCheckOutput("off", `{"rememberIdentifiers":false}`),
				resource.TestCheckOutput("fn_unset", `{"persistentSession":true}`),
				resource.TestCheckOutput("fn_off", `{"rememberIdentifiers":false}`),
			),
		}},
	})
}

// TestAccIssuer_Continuity: both issuer fields reach the server, and an
// issuer that never set remember_identifiers keeps it absent there and
// plans nothing after adding persistent_session.
func TestAccIssuer_Continuity(t *testing.T) {

	h := newHarness(t)

	config := func(extra string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config    = provider::authwise::issuer_config({ %s })
}
`, extra)
	}

	issuer := func(check func(remember *bool, persistent bool) error) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if len(h.fake.issuers) != 1 {
				return fmt.Errorf("expected one issuer on the server, got %d", len(h.fake.issuers))
			}
			for _, i := range h.fake.issuers {
				// Presence is the point, so it is read off the message
				// rather than through the getter, which says false.
				c := i.GetConfig()
				var remember *bool
				if m := c.ProtoReflect(); m.Has(m.Descriptor().Fields().ByName("remember_identifiers")) {
					v := c.GetRememberIdentifiers()
					remember = &v
				}
				return check(remember, c.GetPersistentSession())
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: issuer(func(remember *bool, persistent bool) error {
					if remember != nil || persistent {
						return fmt.Errorf("remember_identifiers = %v, persistent_session = %v; want absent, false", remember, persistent)
					}
					return nil
				}),
			},
			{
				Config: config("persistent_session = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_issuer.login", plancheck.ResourceActionUpdate),
					},
				},
				Check: issuer(func(remember *bool, persistent bool) error {
					if remember != nil || !persistent {
						return fmt.Errorf("remember_identifiers = %v, persistent_session = %v; want absent, true", remember, persistent)
					}
					return nil
				}),
			},
			{
				Config: config("persistent_session = true, remember_identifiers = false"),
				Check: issuer(func(remember *bool, _ bool) error {
					if remember == nil || *remember {
						return fmt.Errorf("remember_identifiers = %v; want false, present", remember)
					}
					return nil
				}),
			},
			{
				Config:             config("persistent_session = true, remember_identifiers = false"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccRealmAuthenticationPolicy_SessionIdle: session.idle reaches the
// server beside absolute.
func TestAccRealmAuthenticationPolicy_SessionIdle(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_realm" "r" {
  display_name = "Customers"
}

resource "authwise_realm_authentication_policy" "p" {
  realm = authwise_realm.r.name
  floor = { mode = "NONE" }
  session = {
    absolute = "720h"
    idle     = "168h"
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("authwise_realm_authentication_policy.p", "session.idle", "168h"),
				checkServer(func() error {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					r := onlyRealm(t, h.fake.realms)
					s := r.GetConfig().GetAuthentication().GetSession()
					if got := s.GetIdle().AsDuration(); got != 168*time.Hour {
						return fmt.Errorf("session.idle = %v", got)
					}
					if got := s.GetAbsolute().AsDuration(); got != 720*time.Hour {
						return fmt.Errorf("session.absolute = %v", got)
					}
					return nil
				}),
			),
		}},
		CheckDestroy: noneLeft(h),
	})
}
