// Client ID Metadata Documents (kit#413, tf#62; apis v0.53.0): an issuer
// may accept an https URL as a client_id.
package acctest_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccIssuerConfig_Cimd: cimd left out is absent from the JSON, and the
// data source and the function encode it alike.
func TestAccIssuerConfig_Cimd(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
data "authwise_issuer_config" "claude" {
  cimd = {
    enabled         = true
    trusted_domains = ["claude.ai"]
  }
}

output "unset" {
  value = provider::authwise::issuer_config({ persistent_session = true })
}

output "claude" {
  value = provider::authwise::issuer_config({ cimd = { enabled = true, trusted_domains = ["claude.ai"] } })
}

output "same" {
  value = provider::authwise::issuer_config({ cimd = { enabled = true, trusted_domains = ["claude.ai"] } }) == data.authwise_issuer_config.claude.json
}

output "off" {
  value = provider::authwise::issuer_config({ cimd = { enabled = false } })
}

output "open" {
  value = provider::authwise::issuer_config({ cimd = { enabled = true } })
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("unset", `{"persistentSession":true}`),
				resource.TestCheckOutput("claude", `{"cimd":{"enabled":true,"trustedDomains":["claude.ai"]}}`),
				resource.TestCheckOutput("same", "true"),
				resource.TestCheckOutput("off", `{"cimd":{}}`),
				resource.TestCheckOutput("open", `{"cimd":{"enabled":true}}`),
			),
		}},
	})
}

// TestAccIssuer_Cimd: an issuer without cimd keeps it absent on the server
// and plans nothing; turned on for claude.ai, it reaches the server, and
// the plan after apply is empty.
func TestAccIssuer_Cimd(t *testing.T) {

	h := newHarness(t)

	config := func(extra string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config    = provider::authwise::issuer_config({ access_token_expire_seconds = 900 %s })
}
`, extra)
	}

	server := func(check func(enabled bool, domains []string, set bool) error) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if len(h.fake.issuers) != 1 {
				return fmt.Errorf("expected one issuer on the server, got %d", len(h.fake.issuers))
			}
			for _, i := range h.fake.issuers {
				c := i.GetConfig().GetCimd()
				return check(c.GetEnabled(), c.GetTrustedDomains(), c != nil)
			}
			return nil
		})
	}

	const claude = `, cimd = { enabled = true, trusted_domains = ["claude.ai"] }`
	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: server(func(_ bool, _ []string, set bool) error {
					if set {
						return fmt.Errorf("cimd is set on the server; want it absent")
					}
					return nil
				}),
			},
			{Config: config(""), ConfigPlanChecks: empty},
			{
				Config: config(claude),
				Check: server(func(enabled bool, domains []string, _ bool) error {
					if !enabled || !slices.Equal(domains, []string{"claude.ai"}) {
						return fmt.Errorf("cimd = enabled %v, trusted_domains %q; want true, [claude.ai]", enabled, domains)
					}
					return nil
				}),
			},
			{Config: config(claude), ConfigPlanChecks: empty},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccInteractiveClientConfig_NoMetadataDocument: kit writes
// metadata_document on the clients it derives from a document, so the
// config builder takes none (tf#63).
func TestAccInteractiveClientConfig_NoMetadataDocument(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.providerConfig() + `
data "authwise_interactive_client_config" "x" {
  metadata_document = { url = "https://claude.ai/oauth/client.json" }
}
`,
			ExpectError: regexp.MustCompile(`(?s)Unsupported\s+argument.*metadata_document`),
		}},
	})
}
