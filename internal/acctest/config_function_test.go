package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccConfigFunction_QuietIssuerPlan is #55. Terraform reads a data
// source at apply when a resource named in its arguments has a change
// planned, so an issuer whose config came from data.authwise_issuer_config
// planned a no-op update on every provider edit. The function is evaluated
// at plan: changing the provider it names, here by adding a signing
// certificate and renaming it, leaves the issuer alone.
func TestAccConfigFunction_QuietIssuerPlan(t *testing.T) {

	h := newHarness(t)

	config := func(withCert bool, display string) string {
		cert, certRef := "", ""
		if withCert {
			cert = `
resource "authwise_certificate" "sp" {
  display_name        = "SP signing"
  use                 = "CERTIFICATE_USE_SIGNING"
  subject_common_name = "sp.example.com"
}
`
			certRef = "signing_certificate_id = authwise_certificate.sp.name"
		}
		return h.providerConfig() + cert + fmt.Sprintf(`
resource "authwise_realm" "partner" {
  display_name = "Partner"
}

data "authwise_provider_saml" "x" {
  idp_entity_id = "https://idp.example.com"
  idp_sso_url   = "https://idp.example.com/sso"
  %s
}

resource "authwise_provider" "saml" {
  realm_id      = authwise_realm.partner.realm_id
  provider_type = "saml"
  display_name  = %q
  config        = data.authwise_provider_saml.x.any
}

resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config = provider::authwise::issuer_config({
    multi_realm_provider_selector = {
      realm_names    = [authwise_realm.partner.name]
      default_target = { realm_name = authwise_realm.partner.name, provider_name = authwise_provider.saml.name }
    }
  })
}
`, certRef, display)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: config(false, "SAML")},
			{
				Config: config(true, "SAML 2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_provider.saml", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("authwise_issuer.login", plancheck.ResourceActionNoop),
					},
				},
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccConfigFunction_MatchesDataSource: for the same arguments a
// function returns exactly the data source's `json`, durations, nested
// lists, JSON-string fields and maps included.
func TestAccConfigFunction_MatchesDataSource(t *testing.T) {

	h := newHarness(t)

	const realm = `{
  default_locale = "fr"
  provisioning = {
    self_signup         = true
    admin_direct        = false
    invitation_ttl      = "72h"
    password_validators = [
      { name = "length", type = "length", config = jsonencode({ minLength = 12 }) },
    ]
  }
  recovery = { self_service_reset = true, reset_ttl = "30m" }
  sms      = { allowed_regions = ["US", "CA"], sends_per_prefix_per_hour = 50 }
}`

	const issuer = `{
  access_token_expire_seconds = 600
  multi_realm_provider_selector = {
    realm_names = ["tenants/t-1/realms/r-1", "tenants/t-1/realms/r-2"]
    identifier  = { kind = "EMAIL" }
    rules = [
      { name = "partner", domains = ["partner.example"], target = { realm_name = "tenants/t-1/realms/r-2" } },
    ]
    default_target = { realm_name = "tenants/t-1/realms/r-1" }
  }
}`

	config := h.providerConfig() + fmt.Sprintf(`
data "authwise_realm_config" "x" %s

data "authwise_issuer_config" "x" %s

output "realm" {
  value = provider::authwise::realm_config(%s) == data.authwise_realm_config.x.json
}

output "issuer" {
  value = provider::authwise::issuer_config(%s) == data.authwise_issuer_config.x.json
}

output "empty" {
  value = provider::authwise::tenant_config({})
}
`, realm, issuer, realm, issuer)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("realm", "true"),
				resource.TestCheckOutput("issuer", "true"),
				resource.TestCheckOutput("empty", "{}"),
			),
		}},
	})
}

// TestAccConfigFunction_Refusals: the function refuses what the data
// source refuses, at plan, naming the attribute.
func TestAccConfigFunction_Refusals(t *testing.T) {

	cases := map[string]struct {
		call string
		want *regexp.Regexp
	}{
		"misspelled attribute": {
			call: `provider::authwise::endpoint_auth({ kitToken = { issuer = "tenants/t-01/issuers/i-01" } })`,
			want: regexp.MustCompile(`(?s)unsupported\s+attribute\s+"kitToken"`),
		},
		"misspelled nested attribute": {
			call: `provider::authwise::realm_config({ recovery = { reset = true } })`,
			want: regexp.MustCompile(`(?s)recovery:\s+unsupported\s+attribute\s+"reset"`),
		},
		"the policy is not realm config": {
			call: `provider::authwise::realm_config({ authentication = {} })`,
			want: regexp.MustCompile(`(?s)unsupported\s+attribute\s+"authentication"`),
		},
		"wrong type": {
			call: `provider::authwise::realm_config({ recovery = { self_service_reset = "yes" } })`,
			want: regexp.MustCompile(`(?s)recovery\.self_service_reset:\s+must\s+be\s+a\s+bool,\s+not\s+a\s+string`),
		},
		"unknown enum value": {
			call: `provider::authwise::issuer_config({ multi_realm_provider_selector = { identifier = { kind = "PHONE" } } })`,
			want: regexp.MustCompile(`(?s)"PHONE"\s+is\s+not\s+a`),
		},
		"bad duration": {
			call: `provider::authwise::realm_config({ recovery = { reset_ttl = "half an hour" } })`,
			want: regexp.MustCompile(`(?s)invalid\s+duration`),
		},
		"two selectors": {
			call: `provider::authwise::issuer_config({
    single_realm_provider_selector = { realm_name = "tenants/t-01/realms/r-01" }
    external_provider_selector     = { endpoint_name = "tenants/t-01/endpoints/e-01" }
  })`,
			want: regexp.MustCompile(`(?s)At\s+most\s+one\s+of`),
		},
		"a reserved provisioning field set": {
			call: `provider::authwise::realm_config({ provisioning = { allowed_provider_types = ["saml"] } })`,
			want: regexp.MustCompile(`(?s)not\s+yet\s+supported.*kit#222`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config:      h.providerConfig() + fmt.Sprintf("\noutput \"x\" {\n  value = %s\n}\n", v.call),
					ExpectError: v.want,
				}},
			})
		})
	}
}
