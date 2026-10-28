// The issuer's preferred domain (apis v0.45.0, kit#857, #52): the host kit
// builds mailed links on, one of the issuer's own domains, set by
// authwise_issuer_preferred_domain and read back on authwise_issuer.
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

// preferredDomainConfig renders an issuer on a native domain, a second
// native domain of the tenant, a custom domain attached to the issuer, and
// the preferred domain naming the given domain resource ("" for none). ttl
// changes the issuer's config so a step can update it alongside.
func preferredDomainConfig(h *harness, preferred string, ttl int) string {
	cfg := h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_domain" "other" {
  hostname = "other.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config    = jsonencode({ accessTokenExpireSeconds = %d })
}

resource "authwise_domain" "custom" {
  hostname  = "auth.example.com"
  issuer_id = authwise_issuer.login.issuer_id
}
`, ttl)
	if preferred != "" {
		cfg += fmt.Sprintf(`
resource "authwise_issuer_preferred_domain" "login" {
  issuer    = authwise_issuer.login.name
  domain_id = authwise_domain.%s.domain_id
}
`, preferred)
	}
	return cfg
}

// fakePreferredDomain is preferred_domain_id on the only issuer.
func fakePreferredDomain(h *harness) (string, error) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if len(h.fake.issuers) != 1 {
		return "", fmt.Errorf("%d issuers, want 1", len(h.fake.issuers))
	}
	for _, i := range h.fake.issuers {
		return i.GetPreferredDomainId(), nil
	}
	return "", nil
}

// fakeDomainID is the id of the domain with the given hostname.
func fakeDomainID(h *harness, hostname string) string {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	for name, d := range h.fake.domains {
		if d.GetHostname() == hostname {
			return name[strings.LastIndex(name, "/")+1:]
		}
	}
	return ""
}

// clearFakePreferredDomain is kit releasing the domain, or anyone setting
// the issuer back to native outside Terraform.
func clearFakePreferredDomain(h *harness) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	for _, i := range h.fake.issuers {
		i.PreferredDomainId = ""
	}
}

// TestAccIssuerPreferredDomain walks the resource: a custom domain in one
// apply with no cycle, a move to the native domain, a domain that is not
// the issuer's, kit clearing it, import, and destroy alongside an update
// of the issuer.
func TestAccIssuerPreferredDomain(t *testing.T) {
	h := newHarness(t)

	prefers := func(hostname string) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			got, err := fakePreferredDomain(h)
			if err != nil {
				return err
			}
			want := ""
			if hostname != "" {
				want = fakeDomainID(h, hostname)
			}
			if got != want {
				return fmt.Errorf("fake preferred_domain_id = %q, want %q (%s)", got, want, hostname)
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// The custom domain names the issuer and the preferred
				// domain names both: one apply, no cycle. The issuer reads
				// it back at the next refresh.
				Config: preferredDomainConfig(h, "custom", 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					prefers("auth.example.com"),
					resource.TestCheckResourceAttrPair("authwise_issuer_preferred_domain.login", "domain_id", "authwise_domain.custom", "domain_id"),
				),
			},
			{
				Config: preferredDomainConfig(h, "custom", 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttrPair("authwise_issuer.login", "preferred_domain_id", "authwise_domain.custom", "domain_id"),
			},
			{
				// The native domain, in place.
				Config: preferredDomainConfig(h, "login", 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("authwise_issuer_preferred_domain.login", plancheck.ResourceActionUpdate)},
				},
				Check: prefers("login.example.com"),
			},
			{
				// Another domain of the tenant is not the issuer's.
				Config:      preferredDomainConfig(h, "other", 600),
				ExpectError: regexp.MustCompile(`not one of this issuer's domains`),
			},
			{
				Config: preferredDomainConfig(h, "custom", 600),
				Check:  prefers("auth.example.com"),
			},
			{
				// kit cleared it (the domain released, or a reset outside
				// Terraform): the refresh finds the resource gone and the
				// plan sets it again.
				PreConfig: func() { clearFakePreferredDomain(h) },
				Config:    preferredDomainConfig(h, "custom", 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("authwise_issuer_preferred_domain.login", plancheck.ResourceActionCreate)},
				},
				Check: prefers("auth.example.com"),
			},
			{
				ResourceName:                         "authwise_issuer_preferred_domain.login",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "issuer",
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_issuer.login"].Primary.Attributes["name"], nil
				},
			},
			{
				// Destroy sets the issuer back to native, while the issuer
				// itself updates in the same apply.
				Config: preferredDomainConfig(h, "", 900),
				Check: resource.ComposeAggregateTestCheckFunc(
					prefers(""),
					resource.TestCheckNoResourceAttr("authwise_issuer.login", "preferred_domain_id"),
				),
			},
		},
	})
}
