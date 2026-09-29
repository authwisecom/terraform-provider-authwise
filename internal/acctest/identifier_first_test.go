// Identifier-first login (apis v0.11.0, #24): the issuer's routing
// selector, written through authwise_issuer's config JSON.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// selectorConfig renders two realms, a provider in the second, and an
// issuer whose selector is the given HCL object.
func selectorConfig(h *harness, selector string) string {
	return h.providerConfig() + `
resource "authwise_realm" "consumers" {
  display_name = "Consumers"
}

resource "authwise_realm" "partner" {
  display_name = "Partner"
}

resource "authwise_provider" "partner_saml" {
  realm_id      = authwise_realm.partner.realm_id
  provider_type = "saml"
}

locals {
  consumers = authwise_realm.consumers.name
  partner   = authwise_realm.partner.name
}

resource "authwise_issuer" "login" {
  domain_name = "login.example.com"
  config      = jsonencode({ multiRealmProviderSelector = ` + selector + ` })
}
`
}

func onlySelector(h *harness) (*corepb.MultiRealmProviderSelector, error) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if len(h.fake.issuers) != 1 {
		return nil, fmt.Errorf("%d issuers, want 1", len(h.fake.issuers))
	}
	for _, i := range h.fake.issuers {
		return i.GetConfig().GetMultiRealmProviderSelector(), nil
	}
	return nil, nil
}

// TestAccIssuer_RoutingSelector writes a selector, changes a rule, and
// checks both land whole — the member is never patched below its root.
func TestAccIssuer_RoutingSelector(t *testing.T) {

	h := newHarness(t)

	selector := func(domains string) string {
		return fmt.Sprintf(`{
    realmNames = [local.consumers, local.partner]
    identifier = { label = "Email", kind = "EMAIL" }
    rules = [
      {
        name    = "partner"
        domains = %s
        target  = { realmName = local.partner, providerName = authwise_provider.partner_saml.name }
      },
      {
        name      = "contractors"
        condition = "local_part.endsWith(\".ext\")"
        target    = { realmName = local.partner }
      },
    ]
    defaultTarget = { realmName = local.consumers }
  }`, domains)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// kind = "EMAIL" is not the zero value, so it round-trips.
				Config: selectorConfig(h, selector(`["partner.example"]`)),
				Check: checkServer(func() error {
					s, err := onlySelector(h)
					if err != nil {
						return err
					}
					if s.GetIdentifier().GetKind() != corepb.IdentifierField_EMAIL || len(s.GetRules()) != 2 ||
						s.GetRules()[1].GetCondition() == "" {
						return fmt.Errorf("selector = %v", s)
					}
					return nil
				}),
			},
			{
				Config: selectorConfig(h, selector(`["partner.example", "*.partner.example"]`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_issuer.login", plancheck.ResourceActionUpdate),
					},
				},
				Check: checkServer(func() error {
					s, err := onlySelector(h)
					if err != nil {
						return err
					}
					if got := s.GetRules()[0].GetDomains(); len(got) != 2 || got[1] != "*.partner.example" {
						return fmt.Errorf("domains after update = %v", got)
					}
					return nil
				}),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccIssuer_RoutingSelectorRefusals pins what kit refuses, and the
// JSON-lane caveat the docs describe: an explicit zero is omitted on read.
func TestAccIssuer_RoutingSelectorRefusals(t *testing.T) {

	type s struct {
		selector string
		want     *regexp.Regexp
	}

	cases := map[string]s{
		"realm_names alone": {
			selector: `{ realmNames = [local.consumers] }`,
			want:     regexp.MustCompile(`default_target: required`),
		},
		"a target outside realm_names": {
			selector: `{
    realmNames    = [local.consumers]
    defaultTarget = { realmName = local.partner }
  }`,
			want: regexp.MustCompile(`default_target.realm_name`),
		},
		"a domain rule on a USERNAME identifier": {
			selector: `{
    realmNames    = [local.consumers, local.partner]
    identifier    = { kind = "USERNAME" }
    rules         = [{ name = "p", domains = ["partner.example"], target = { realmName = local.partner } }]
    defaultTarget = { realmName = local.consumers }
  }`,
			want: regexp.MustCompile(`rules\[0\].domains`),
		},
		"a rule with neither domains nor a condition": {
			selector: `{
    realmNames    = [local.consumers, local.partner]
    rules         = [{ name = "p", target = { realmName = local.partner } }]
    defaultTarget = { realmName = local.consumers }
  }`,
			want: regexp.MustCompile(`rules\[0\]: a rule needs`),
		},
		// Not a refusal: kit stores KIND_UNSPECIFIED and, like every zero
		// value, omits it on read, so the JSON terraform compares no longer
		// matches what was written. The docs say to leave kind out instead.
		"an explicit KIND_UNSPECIFIED": {
			selector: `{
    realmNames    = [local.consumers]
    identifier    = { label = "Email", kind = "KIND_UNSPECIFIED" }
    defaultTarget = { realmName = local.consumers }
  }`,
			want: regexp.MustCompile(`inconsistent result after apply`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{
						Config:      selectorConfig(h, v.selector),
						ExpectError: v.want,
					},
				},
			})
		})
	}
}
