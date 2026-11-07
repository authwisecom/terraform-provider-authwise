// Tenant domains through bindings (kit#911, tf#69): a domain names no
// issuer; authwise_issuer_domain_binding puts an issuer on a domain at a
// path, and authwise_issuer_preferred_binding picks the one kit mails
// links on.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// issuerBindingConfig is a native and a custom domain of the tenant, an issuer
// with no binding of its own, its bindings on the domains named, and the
// preferred binding ("" for none). ttl changes the issuer's config so a
// step can update it alongside.
func issuerBindingConfig(h *harness, bound []string, preferred string, ttl int) string {
	cfg := h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "native" {
  hostname = "acme.id.authwise.test"
}

resource "authwise_domain" "custom" {
  hostname = "login.acme.com"
  custom   = true
}

resource "authwise_issuer" "login" {
  config = jsonencode({ accessTokenExpireSeconds = %d })
}
`, ttl)
	for _, d := range bound {
		cfg += fmt.Sprintf(`
resource "authwise_issuer_domain_binding" "%[1]s" {
  issuer_id = authwise_issuer.login.issuer_id
  domain_id = authwise_domain.%[1]s.domain_id
  %[2]s
}
`, d, map[bool]string{true: `path = ""`}[d == "native"])
	}
	if preferred != "" {
		cfg += fmt.Sprintf(`
resource "authwise_issuer_preferred_binding" "login" {
  issuer     = authwise_issuer.login.name
  binding_id = authwise_issuer_domain_binding.%s.issuer_domain_binding_id
}
`, preferred)
	}
	return cfg
}

// fakeIssuer is the only issuer, as kit reads it.
func fakeIssuer(h *harness) (preferred string, bindings int, err error) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if len(h.fake.issuers) != 1 {
		return "", 0, fmt.Errorf("%d issuers, want 1", len(h.fake.issuers))
	}
	for _, i := range h.fake.issuers {
		return i.GetPreferredBindingId(), len(h.fake.bindingsOfLocked(i.GetName())), nil
	}
	return "", 0, nil
}

// TestAccIssuerDomainBinding walks an issuer onto two domains and a
// preferred binding in one apply, an update of the issuer beside them,
// import, kit clearing the preference, and unbinding in the order kit
// requires.
func TestAccIssuerDomainBinding(t *testing.T) {
	h := newHarness(t)

	prefers := func(addr string, n int) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			got, bindings, err := fakeIssuer(h)
			if err != nil {
				return err
			}
			want := ""
			if addr != "" {
				want = s.RootModule().Resources[addr].Primary.Attributes["issuer_domain_binding_id"]
			}
			if got != want || bindings != n {
				return fmt.Errorf("fake preferred_binding_id = %q with %d bindings, want %q with %d", got, bindings, want, n)
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Two hosts for one issuer, each its own iss, and the
				// preferred binding naming the custom one: one apply, no
				// cycle.
				Config: issuerBindingConfig(h, []string{"native", "custom"}, "custom", 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					prefers("authwise_issuer_domain_binding.custom", 2),
					resource.TestCheckResourceAttr("authwise_issuer_domain_binding.custom", "url", "https://login.acme.com"),
					resource.TestCheckNoResourceAttr("authwise_issuer_domain_binding.custom", "path"),
					resource.TestCheckResourceAttr("authwise_issuer_domain_binding.native", "path", ""),
					resource.TestCheckResourceAttr("authwise_issuer_domain_binding.custom", "routable", "false"),
					resource.TestCheckResourceAttr("authwise_issuer_domain_binding.native", "routable", "true"),
					resource.TestCheckResourceAttrPair("authwise_issuer_domain_binding.custom", "issuer_id", "authwise_issuer.login", "issuer_id"),
				),
			},
			{
				// The issuer reads its bindings and the preference back,
				// with nothing to change.
				Config: issuerBindingConfig(h, []string{"native", "custom"}, "custom", 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("authwise_issuer.login", "preferred_binding_id", "authwise_issuer_domain_binding.custom", "issuer_domain_binding_id"),
					resource.TestCheckResourceAttr("authwise_issuer.login", "bindings.#", "2"),
					// domain_name is the first binding's host; the two were
					// made in one apply, in either order.
					resource.TestMatchResourceAttr("authwise_issuer.login", "domain_name", regexp.MustCompile(`^(acme\.id\.authwise\.test|login\.acme\.com)$`)),
				),
			},
			{
				ResourceName:                         "authwise_issuer_domain_binding.custom",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_issuer_domain_binding.custom"].Primary.Attributes["name"], nil
				},
			},
			{
				// kit cleared it (the binding deleted outside Terraform, or
				// a reset): the refresh finds the resource gone and the
				// plan sets it again.
				PreConfig: func() {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					for _, i := range h.fake.issuers {
						i.PreferredBindingId = ""
					}
				},
				Config: issuerBindingConfig(h, []string{"native", "custom"}, "custom", 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("authwise_issuer_preferred_binding.login", plancheck.ResourceActionCreate)},
				},
				Check: prefers("authwise_issuer_domain_binding.custom", 2),
			},
			{
				// Unbinding the custom host destroys the preference first,
				// while the issuer updates in the same apply.
				Config: issuerBindingConfig(h, []string{"native"}, "", 900),
				Check: resource.ComposeAggregateTestCheckFunc(
					prefers("", 1),
					resource.TestCheckResourceAttr("authwise_issuer.login", "bindings.#", "1"),
				),
			},
		},
	})
}

// TestAccIssuerDomainBinding_Refusals: one host and path serve one issuer
// (BINDING_TAKEN); only the root is routed and /admin is reserved; a
// product's host takes no issuer.
func TestAccIssuerDomainBinding_Refusals(t *testing.T) {
	h := newHarness(t)

	base := h.providerConfig() + `
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_domain" "guard" {
  hostname = "guard.acme.com"
  product  = "guard"
}

resource "authwise_issuer" "first" {
  domain_id = authwise_domain.login.domain_id
}

resource "authwise_issuer" "second" {}
`
	bind := func(domain, path string) string {
		return base + fmt.Sprintf(`
resource "authwise_issuer_domain_binding" "second" {
  issuer_id = authwise_issuer.second.issuer_id
  domain_id = authwise_domain.%s.domain_id
  path      = %q
}
`, domain, path)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: base},
			{
				// The first issuer's convenience domain_id made its
				// binding at the root; a second issuer there is refused.
				Config:      bind("login", ""),
				ExpectError: regexp.MustCompile(`(?s)BINDING_TAKEN.*already answers at\s+login\.example\.com\s+at\s+its\s+root`),
			},
			{
				Config:      bind("login", "/admin"),
				ExpectError: regexp.MustCompile(`reserved`),
			},
			{
				Config:      bind("login", "/customers"),
				ExpectError: regexp.MustCompile(`kit#876`),
			},
			{
				Config:      bind("guard", ""),
				ExpectError: regexp.MustCompile(`DOMAIN_NOT_BINDABLE`),
			},
		},
	})
}

// TestAccDomain_InUse: kit refuses deleting a domain an issuer is bound to
// (DOMAIN_IN_USE); the provider says which binding to destroy first.
func TestAccDomain_InUse(t *testing.T) {
	h := newHarness(t)

	issuer := h.providerConfig() + `
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: issuer},
			{
				// The issuer's binding outlives a removed domain block: kit
				// refuses, naming the issuer.
				Config: h.providerConfig() + `
resource "authwise_issuer" "login" {}
`,
				ExpectError: regexp.MustCompile(`(?s)DOMAIN_IN_USE.*authwise_issuer_domain_binding`),
			},
			{Config: issuer},
		},
	})
}
