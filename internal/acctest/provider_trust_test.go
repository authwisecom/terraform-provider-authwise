// tf#83, following kit#1010 and kit#1011 (apis v0.86.0): a provider's
// trusted_email_domains, the upstream_acr_map key forms, and the plan-time
// refusals that mirror kit's.
package acctest_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const (
	samlTrustAddr = "authwise_provider.partner"
	entraAcrKey   = "attribute:http://schemas.microsoft.com/claims/authnmethodsreferences=http://schemas.microsoft.com/claims/multipleauthn"
)

// samlTrustConfig is a SAML provider with the given extra lines, and the
// data source reading it back.
func (h *harness) samlTrustConfig(lines string) string {
	return h.providerConfig() + realmForFactors + `
data "authwise_provider_saml" "partner" {
  idp_entity_id = "https://sts.windows.net/tenant/"
  idp_sso_url   = "https://login.microsoftonline.com/tenant/saml2"
}

resource "authwise_provider" "partner" {
  display_name  = "Contoso staff"
  realm_id      = local.realm_id
  provider_type = "saml"
  config        = data.authwise_provider_saml.partner.any
` + lines + `}

data "authwise_provider" "partner" {
  name = authwise_provider.partner.name
}
`
}

// serverProvider checks the one provider row on the fake.
func (h *harness) serverProvider(check func(domains []string, acr map[string]string, trust, link bool) error) resource.TestCheckFunc {
	return checkServer(func() error {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		for _, p := range h.fake.providers {
			return check(p.GetTrustedEmailDomains(), p.GetUpstreamAcrMap(), p.GetTrustUpstreamEmailVerified(), p.GetLinkByVerifiedEmail())
		}
		return fmt.Errorf("no provider on the server")
	})
}

// TestAccProvider_TrustedEmailDomains: on a saml provider the list makes
// both email settings acceptable, changes in place, reaches kit's row and
// the data source, and is cleared by removing its line.
func TestAccProvider_TrustedEmailDomains(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.samlTrustConfig(`
  trusted_email_domains         = ["contoso.com"]
  trust_upstream_email_verified = true
  link_by_verified_email        = true
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(samlTrustAddr, "trusted_email_domains.#", "1"),
					resource.TestCheckResourceAttr(samlTrustAddr, "trusted_email_domains.0", "contoso.com"),
					resource.TestCheckResourceAttr("data.authwise_provider.partner", "trusted_email_domains.0", "contoso.com"),
					h.serverProvider(func(d []string, _ map[string]string, trust, link bool) error {
						if !slices.Equal(d, []string{"contoso.com"}) || !trust || !link {
							return fmt.Errorf("row: domains %v, trust %t, link %t", d, trust, link)
						}
						return nil
					}),
				),
			},
			{
				Config: h.samlTrustConfig(`
  trusted_email_domains         = ["contoso.com", "contoso.co.uk"]
  trust_upstream_email_verified = true
  link_by_verified_email        = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(samlTrustAddr, plancheck.ResourceActionUpdate)},
				},
				Check: h.serverProvider(func(d []string, _ map[string]string, _, _ bool) error {
					if !slices.Equal(d, []string{"contoso.com", "contoso.co.uk"}) {
						return fmt.Errorf("row domains %v", d)
					}
					return nil
				}),
			},
			{
				// The list, the settings and the line all removed: kit's
				// row is cleared in place.
				Config: h.samlTrustConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(samlTrustAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(samlTrustAddr, "trusted_email_domains"),
					h.serverProvider(func(d []string, _ map[string]string, trust, link bool) error {
						if len(d) != 0 || trust || link {
							return fmt.Errorf("row: domains %v, trust %t, link %t", d, trust, link)
						}
						return nil
					}),
				),
			},
			{
				Config:           h.samlTrustConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccProvider_UpstreamAcrMapEntra: the Entra recipe's attribute key is
// accepted and reaches kit's row as written.
func TestAccProvider_UpstreamAcrMapEntra(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.samlTrustConfig(`
  trust_upstream_amr = true
  upstream_acr_map = {
    "` + entraAcrKey + `" = "urn:authwise:acr:2"
  }
`),
				Check: h.serverProvider(func(_ []string, acr map[string]string, _, _ bool) error {
					if acr[entraAcrKey] != "urn:authwise:acr:2" || len(acr) != 1 {
						return fmt.Errorf("row upstream_acr_map %v", acr)
					}
					return nil
				}),
			},
			{
				Config: h.samlTrustConfig(`
  trust_upstream_amr = true
  upstream_acr_map = {
    "` + entraAcrKey + `" = "urn:authwise:acr:2"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccProvider_TrustRefusals: what kit refuses on write is refused at
// plan, before anything reaches kit.
func TestAccProvider_TrustRefusals(t *testing.T) {

	cases := map[string]struct {
		lines string
		want  *regexp.Regexp
	}{
		"trust_upstream_email_verified on saml without the list": {
			lines: "  trust_upstream_email_verified = true\n",
			want:  regexp.MustCompile(`(?s)trust_upstream_email_verified on a saml provider needs\s+trusted_email_domains`),
		},
		"link_by_verified_email on saml without the list": {
			lines: "  link_by_verified_email = true\n",
			want:  regexp.MustCompile(`(?s)link_by_verified_email on a saml provider needs\s+trusted_email_domains`),
		},
		"link_by_verified_email on saml with an empty list": {
			lines: "  link_by_verified_email = true\n  trusted_email_domains = []\n",
			want:  regexp.MustCompile(`(?s)link_by_verified_email on a saml provider needs\s+trusted_email_domains`),
		},
		"a wildcard domain": {
			lines: `  trusted_email_domains = ["*.contoso.com"]` + "\n",
			want:  regexp.MustCompile(`(?s)a lowercase domain name such as\s+"contoso.com"`),
		},
		"an upper-case domain": {
			lines: `  trusted_email_domains = ["Contoso.com"]` + "\n",
			want:  regexp.MustCompile(`(?s)a lowercase domain name`),
		},
		"an address, not a domain": {
			lines: `  trusted_email_domains = ["it@contoso.com"]` + "\n",
			want:  regexp.MustCompile(`(?s)a lowercase domain name`),
		},
		"a repeated domain": {
			lines: `  trusted_email_domains = ["contoso.com", "contoso.com"]` + "\n",
			want:  regexp.MustCompile(`(?s)duplicate`),
		},
		"an attribute key with no value": {
			lines: `  upstream_acr_map = { "attribute:amr" = "urn:authwise:acr:2" }` + "\n",
			want:  regexp.MustCompile(`(?s)Invalid upstream_acr_map key`),
		},
		"an entry with no level": {
			lines: `  upstream_acr_map = { "urn:example:mfa" = "" }` + "\n",
			want:  regexp.MustCompile(`(?s)upstream_acr_map`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{Config: h.samlTrustConfig(v.lines), PlanOnly: true, ExpectError: v.want},
				},
			})
		})
	}
}

// TestAccProvider_TrustedEmailDomainsOnMagicLink: a type with no upstream
// takes no list.
func TestAccProvider_TrustedEmailDomainsOnMagicLink(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.providerConfig() + `
data "authwise_provider_magic_link" "c" {}

resource "authwise_provider" "p" {
  display_name          = "Sign in"
  realm_id              = "r-1"
  provider_type         = "magicLink"
  config                = data.authwise_provider_magic_link.c.any
  trusted_email_domains = ["contoso.com"]
}`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`(?s)trusted_email_domains is for a federated provider`),
		}},
	})
}

// TestAccProvider_TrustedEmailDomainsEarlierKit: a kit without
// identity.provider-upstream-trust would drop the list, so a write that
// sets it is refused by name; the provider is otherwise managed.
func TestAccProvider_TrustedEmailDomainsEarlierKit(t *testing.T) {

	h := newHarness(t)
	h.fake.capabilities = func() (*identitypb.InstallCapabilities, error) {
		return &identitypb.InstallCapabilities{
			Features:   []string{"guard.tenant-collapse", "identity.object-status", "identity.vocabulary"},
			KitVersion: "1.97.4",
		}, nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: h.samlTrustConfig("")},
			{
				Config:      h.samlTrustConfig(`  trusted_email_domains = ["contoso.com"]` + "\n"),
				ExpectError: regexp.MustCompile(`(?s)identity\.provider-upstream-trust.*kit 1\.97\.4`),
			},
		},
	})
}
