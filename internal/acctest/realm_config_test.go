package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccRealm_SignupConfig: the realm config blocks of kit v1.34.0 and
// v1.36.0 (apis v0.19.0, v0.20.0) ride the realm's JSON config as written:
// recovery, and the bot_protection and sms of kit's signup-realm guide, with
// the secret named by reference. The second plan is empty, so nothing drifts.
func TestAccRealm_SignupConfig(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_secret" "turnstile" {
  display_name = "Turnstile secret"
  payload_wo   = "turnstile-secret-material"
}

resource "authwise_realm" "signup" {
  display_name = "Customers"
  config = jsonencode({
    provisioning = { selfSignup = true, requireEmailVerification = true }
    recovery = {
      selfServiceReset = true
      resetTtl         = "1800s"
      supportContact   = "help@example.com"
    }
    botProtection = {
      mode                   = "ALWAYS"
      provider               = "TURNSTILE"
      siteKey                = "0x4AAAAAAA"
      secretRef              = { name = authwise_secret.turnstile.name }
      failMode               = "OPEN"
      timeout                = "2s"
      refuseDisposableSignup = true
    }
    sms = {
      allowedRegions        = ["US", "CA"]
      deniedRegions         = ["KP"]
      sendsPerPrefixPerHour = 50
    }
  })
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
					switch {
					case !c.GetRecovery().GetSelfServiceReset() || c.GetRecovery().GetResetTtl().GetSeconds() != 1800:
						return fmt.Errorf("recovery on the row = %v", c.GetRecovery())
					case bp.GetMode().String() != "ALWAYS" || bp.GetProvider().String() != "TURNSTILE" ||
						bp.GetSecretRef().GetName() == "" || bp.GetFailMode().String() != "OPEN" || !bp.GetRefuseDisposableSignup():
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

// TestAccRealm_SignupConfigRefusals: kit's admission of those blocks reaches
// the practitioner with its own reason.
func TestAccRealm_SignupConfigRefusals(t *testing.T) {

	cases := map[string]struct {
		config string
		want   *regexp.Regexp
	}{
		"adaptive": {
			config: `botProtection = { mode = "ADAPTIVE", provider = "ENDPOINT", endpointName = "bots" }`,
			want:   regexp.MustCompile(`(?s)ADAPTIVE is not\s+implemented yet`),
		},
		"always without a provider": {
			config: `botProtection = { mode = "ALWAYS" }`,
			want:   regexp.MustCompile(`(?s)ALWAYS needs a\s+provider`),
		},
		"vendor without its secret": {
			config: `botProtection = { mode = "ALWAYS", provider = "HCAPTCHA", siteKey = "k" }`,
			want:   regexp.MustCompile(`(?s)bot_protection.secret_ref\s+is\s+required\s+for\s+HCAPTCHA`),
		},
		"region not alpha-2": {
			config: `sms = { deniedRegions = ["usa"] }`,
			want:   regexp.MustCompile(`(?s)sms.denied_regions\s+"usa"\s+is\s+not`),
		},
		"reset link too long-lived": {
			config: `recovery = { selfServiceReset = true, resetTtl = "172800s" }`,
			want:   regexp.MustCompile(`(?s)reset_ttl\s+48h0m0s,\s+outside`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config: h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "signup" {
  display_name = "Customers"
  config       = jsonencode({ %s })
}
`, v.config),
					ExpectError: v.want,
				}},
			})
		})
	}
}
