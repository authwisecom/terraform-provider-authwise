package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccRealmConfig_DataSource: TestAccRealm_SignupConfig's realm config,
// built by authwise_realm_config instead of jsonencode (#38), plus a
// password validator, whose config is a google.protobuf.Struct and so takes
// JSON. The durations are Go strings ("30m") that kit returns as protojson
// ("1800s"); the second plan is empty all the same.
func TestAccRealmConfig_DataSource(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_secret" "turnstile" {
  display_name = "Turnstile secret"
  payload_wo   = "turnstile-secret-material"
}

data "authwise_realm_config" "signup" {
  default_locale = "fr"

  provisioning = {
    self_signup                = true
    require_email_verification = true
    password_validators = [
      { name = "length", type = "length", config = jsonencode({ minLength = 12 }) },
    ]
  }

  recovery = {
    self_service_reset = true
    reset_ttl          = "30m"
    support_contact    = "help@example.com"
  }

  bot_protection = {
    mode                     = "ALWAYS"
    provider                 = "TURNSTILE"
    site_key                 = "0x4AAAAAAA"
    secret_ref               = { name = authwise_secret.turnstile.name }
    fail_mode                = "OPEN"
    timeout                  = "2s"
    refuse_disposable_signup = true
  }

  sms = {
    allowed_regions           = ["US", "CA"]
    sends_per_prefix_per_hour = 50
  }
}

resource "authwise_realm" "signup" {
  display_name = "Customers"
  config       = data.authwise_realm_config.signup.json
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: config,
			Check: checkServer(func() error {
				h.fake.mu.Lock()
				defer h.fake.mu.Unlock()
				for _, r := range h.fake.realms {
					c := r.GetConfig()
					bp := c.GetBotProtection()
					pv := c.GetProvisioning().GetPasswordValidators()
					switch {
					case c.GetDefaultLocale() != "fr":
						return fmt.Errorf("default_locale on the row = %q", c.GetDefaultLocale())
					case !c.GetProvisioning().GetSelfSignup() || len(pv) != 1 ||
						pv[0].GetConfig().GetFields()["minLength"].GetNumberValue() != 12:
						return fmt.Errorf("provisioning on the row = %v", c.GetProvisioning())
					case !c.GetRecovery().GetSelfServiceReset() || c.GetRecovery().GetResetTtl().GetSeconds() != 1800:
						return fmt.Errorf("recovery on the row = %v", c.GetRecovery())
					case bp.GetMode().String() != "ALWAYS" || bp.GetProvider().String() != "TURNSTILE" ||
						bp.GetSecretRef().GetName() == "" || bp.GetFailMode().String() != "OPEN" ||
						bp.GetTimeout().GetSeconds() != 2 || !bp.GetRefuseDisposableSignup():
						return fmt.Errorf("bot_protection on the row = %v", bp)
					case len(c.GetSms().GetAllowedRegions()) != 2 || c.GetSms().GetSendsPerPrefixPerHour() != 50:
						return fmt.Errorf("sms on the row = %v", c.GetSms())
					}
				}
				return nil
			}),
		}},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccConfigJSON_Refusals: what the typed builders catch at plan, before
// anything reaches kit.
func TestAccConfigJSON_Refusals(t *testing.T) {

	cases := map[string]struct {
		config string
		want   *regexp.Regexp
	}{
		"two selectors": {
			config: `
data "authwise_issuer_config" "x" {
  single_realm_provider_selector = { realm_name = "tenants/t-01/realms/r-01" }
  external_provider_selector     = { endpoint_name = "tenants/t-01/endpoints/e-01" }
}`,
			want: regexp.MustCompile(`At most one of`),
		},
		"unknown enum value": {
			config: `
data "authwise_issuer_config" "x" {
  multi_realm_provider_selector = { identifier = { kind = "PHONE" } }
}`,
			want: regexp.MustCompile(`(?s)value must be one of`),
		},
		"bad duration": {
			config: `
data "authwise_realm_config" "x" {
  recovery = { reset_ttl = "half an hour" }
}`,
			want: regexp.MustCompile(`invalid duration`),
		},
		"the policy is not realm config": {
			config: `
data "authwise_realm_config" "x" {
  authentication = {}
}`,
			want: regexp.MustCompile(`(?s)Unsupported argument`),
		},
		"misspelled attribute": {
			config: `
data "authwise_endpoint_auth" "x" {
  kitToken = { issuer = "tenants/t-01/issuers/i-01", audience = "https://x.example.com" }
}`,
			want: regexp.MustCompile(`(?s)Unsupported argument`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config:      h.providerConfig() + v.config,
					ExpectError: v.want,
				}},
			})
		})
	}
}

// TestAccAudienceConfig_DataSource: a proto3 optional keeps its presence, so
// an audience can say it offers no OpenID (scopes.openid = false), which kit
// reads as true when absent.
func TestAccAudienceConfig_DataSource(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
data "authwise_audience_config" "api" {
  access_token_expire_seconds = 600

  scopes = {
    openid        = false
    authorization = { include_roles = true }
  }

  authorization = {
    access = { decision_sampling = 5 }
  }
}

resource "authwise_audience" "api" {
  display_name = "API"
  config       = data.authwise_audience_config.api.json
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.authwise_audience_config.api", "json",
					`{"accessTokenExpireSeconds":600,"authorization":{"access":{"decisionSampling":5}},"scopes":{"authorization":{"includeRoles":true},"openid":false}}`),
				checkServer(func() error {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					for _, a := range h.fake.audiences {
						s := a.GetConfig().GetScopes()
						if s.Openid == nil || s.GetOpenid() || !s.GetAuthorization().GetIncludeRoles() {
							return fmt.Errorf("scopes on the row = %v", s)
						}
					}
					return nil
				}),
			),
		}},
		CheckDestroy: noneLeft(h),
	})
}
