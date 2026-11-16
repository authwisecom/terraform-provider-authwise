// Inbound SCIM's admin resources (tf#18, kit#532): a connection with kit's
// defaults read back, a change made in place, and a token whose secret is
// returned once, kept in state, and replaced (never updated) when an
// argument changes.
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
	scimConnAddr  = "authwise_scim_connection.entra"
	scimTokenAddr = "authwise_scim_token.entra"
)

func (h *harness) scimConfig(onDelete, expires string) string {
	return h.providerConfig() + realmForFactors + fmt.Sprintf(`
resource "authwise_scim_connection" "entra" {
  realm_id     = local.realm_id
  display_name = "Entra ID"
  issuer_id    = "i-1"
  provider_id  = "p-1"
  %s
}

resource "authwise_scim_token" "entra" {
  realm_id           = local.realm_id
  scim_connection_id = authwise_scim_connection.entra.scim_connection_id
  expires_at         = %q
}

data "authwise_scim_connections" "all" {
  realm_id   = local.realm_id
  depends_on = [authwise_scim_connection.entra]
}

data "authwise_scim_tokens" "all" {
  realm_id   = local.realm_id
  depends_on = [authwise_scim_token.entra]
}
`, onDelete, expires)
}

func TestAccScim_ConnectionAndToken(t *testing.T) {

	h := newHarness(t)
	var connName, tokenName, secret string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.scimConfig("", "2030-01-01T00:00:00Z"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(scimConnAddr, "name", regexp.MustCompile(`^tenants/t-1/realms/[^/]+/scim-connections/sc-`)),
					resource.TestCheckResourceAttr(scimConnAddr, "identifier_attribute", "userName"),
					resource.TestCheckResourceAttr(scimConnAddr, "on_delete", "deactivate"),
					resource.TestCheckResourceAttr(scimConnAddr, "status", "active"),
					resource.TestMatchResourceAttr(scimConnAddr, "base_url", regexp.MustCompile(`^https://id\.acme\.example/scim/sc-[^/]+/v2$`)),
					resource.TestMatchResourceAttr(scimTokenAddr, "name", regexp.MustCompile(`/scim-tokens/st-`)),
					resource.TestCheckResourceAttrSet(scimTokenAddr, "created_at"),
					func(s *terraform.State) error {
						attrs := s.RootModule().Resources[scimTokenAddr].Primary.Attributes
						tokenName, secret = attrs["name"], attrs["secret"]
						connName = s.RootModule().Resources[scimConnAddr].Primary.Attributes["name"]
						id := tokenName[strings.LastIndex(tokenName, "/")+1:]
						if !strings.HasPrefix(secret, id+"_") {
							return fmt.Errorf("secret %q is not <id>_<plaintext> for id %q", secret, id)
						}
						return nil
					},
					resource.TestCheckResourceAttr("data.authwise_scim_connections.all", "scim_connections.#", "1"),
					resource.TestCheckResourceAttr("data.authwise_scim_tokens.all", "scim_tokens.#", "1"),
					resource.TestCheckNoResourceAttr("data.authwise_scim_tokens.all", "scim_tokens.0.secret"),
				),
			},
			{
				// on_delete changes in place; the token is untouched.
				Config: h.scimConfig(`on_delete = "delete"`, "2030-01-01T00:00:00Z"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(scimConnAddr, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(scimTokenAddr, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(scimConnAddr, "on_delete", "delete"),
					resource.TestCheckResourceAttrWith(scimConnAddr, "name", equalTo(&connName)),
					resource.TestCheckResourceAttrWith(scimTokenAddr, "secret", equalTo(&secret)),
				),
			},
			{
				// A token has no update: a new expiry is a new token.
				Config: h.scimConfig(`on_delete = "delete"`, "2031-06-30T00:00:00Z"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(scimTokenAddr, plancheck.ResourceActionReplace)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(scimTokenAddr, "name", notEqualTo(&tokenName)),
					resource.TestCheckResourceAttrWith(scimTokenAddr, "secret", notEqualTo(&secret)),
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						if n := len(h.fake.scimTokens); n != 1 {
							return fmt.Errorf("%d scim tokens, want 1", n)
						}
						return nil
					}),
				),
			},
			{
				// A connection imports with nothing to change.
				ResourceName:      scimConnAddr,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importByName(scimConnAddr),
				Config:            h.scimConfig(`on_delete = "delete"`, "2031-06-30T00:00:00Z"),
			},
			{
				// A token's secret cannot be recovered: an import has none.
				ResourceName:                         scimTokenAddr,
				ImportState:                          true,
				ImportStateIdFunc:                    importByName(scimTokenAddr),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"secret", "realm_id"},
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if n := len(h.fake.scimConnections) + len(h.fake.scimTokens); n != 0 {
				return fmt.Errorf("%d SCIM rows left", n)
			}
			return nil
		},
	})
}
