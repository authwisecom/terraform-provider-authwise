// The issuer's account-page client (apis v0.42.0, kit#827, #34): kit
// creates it at the issuer's first audience and reports it in the
// output-only account_client_id, outside config.
package acctest_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// accountClientConfig renders an issuer that sets config, its first
// audience, and a data source read of the issuer ordered after it.
func accountClientConfig(h *harness, ttl int) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config    = jsonencode({ accessTokenExpireSeconds = %d })
}

resource "authwise_audience" "api" {
  issuer_id    = authwise_issuer.login.issuer_id
  display_name = "https://api.example.com"
}

data "authwise_issuer" "login" {
  name       = authwise_issuer.login.name
  depends_on = [authwise_audience.api]
}
`, ttl)
}

// fakeAccountClientID is the account client kit set on the only issuer.
func fakeAccountClientID(h *harness) (string, error) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if len(h.fake.issuers) != 1 {
		return "", fmt.Errorf("%d issuers, want 1", len(h.fake.issuers))
	}
	for _, i := range h.fake.issuers {
		if i.GetAccountClientId() == "" {
			return "", fmt.Errorf("issuer %s has no account client", i.GetName())
		}
		if i.GetConfig().GetAccountClientId() != "" {
			return "", fmt.Errorf("config.account_client_id = %q, want it left to the caller", i.GetConfig().GetAccountClientId())
		}
		return i.GetAccountClientId(), nil
	}
	return "", nil
}

// TestAccIssuer_AccountClient checks the engine-provided account client
// never drifts an issuer that sets config, and where it can be read: the
// apply that creates the first audience sees it only through the data
// source; the resource picks it up at the next refresh.
func TestAccIssuer_AccountClient(t *testing.T) {
	h := newHarness(t)

	matchesFake := func(addr string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			id, err := fakeAccountClientID(h)
			if err != nil {
				return err
			}
			return resource.TestCheckResourceAttr(addr, "account_client_id", id)(s)
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// The issuer is created before the audience that creates
				// the client, so its own attribute is empty this apply.
				Config: accountClientConfig(h, 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("authwise_issuer.login", "account_client_id"),
					matchesFake("data.authwise_issuer.login"),
				),
			},
			{
				// The refresh picks it up with nothing to change.
				Config: accountClientConfig(h, 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: matchesFake("authwise_issuer.login"),
			},
			{
				// Rewriting config leaves the client in place.
				Config: accountClientConfig(h, 900),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_issuer.login", plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue("authwise_issuer.login", tfjsonpath.New("account_client_id"), knownvalue.NotNull()),
					},
				},
				Check: matchesFake("authwise_issuer.login"),
			},
		},
	})
}
