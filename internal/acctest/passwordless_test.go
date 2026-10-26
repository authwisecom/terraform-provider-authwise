// Passwordless primaries (kit#194, kit#195, #21): the magic link and
// passkey config data sources, and the providers built from them.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccProviderMagicLink_RoundTrip is the round trip the issue calls the
// thing that would break: ttl goes in as "15m", reaches kit as 15 minutes
// inside the provider's Any, and every plan after reads the same — the
// data source keeps the spelling, the provider's config compares as JSON.
func TestAccProviderMagicLink_RoundTrip(t *testing.T) {

	h := newHarness(t)

	magicLink := func(ttl, mode, extra string) string {
		return h.providerConfig() + realmForFactors + fmt.Sprintf(`
data "authwise_provider_magic_link" "ml" {
  code_length = 8
  ttl         = %q
  mode        = %q
%s}

resource "authwise_provider" "ml" {
  realm_id      = local.realm_id
  display_name  = "Email link"
  provider_type = "magicLink"
  config        = data.authwise_provider_magic_link.ml.any
}
`, ttl, mode, extra)
	}

	serverConfig := func() (*corepb.ProviderMagicLink, error) {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		if len(h.fake.providers) != 1 {
			return nil, fmt.Errorf("%d providers on the server, want 1", len(h.fake.providers))
		}
		for _, p := range h.fake.providers {
			ml := &corepb.ProviderMagicLink{}
			if err := p.GetConfig().UnmarshalTo(ml); err != nil {
				return nil, fmt.Errorf("provider config: %w", err)
			}
			return ml, nil
		}
		return nil, nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: magicLink("15m", "LINK_AND_CODE", `
  link_scope      = "ANY_BROWSER"
  unknown_address = "TELL"
  request_limits  = { per_identifier_per_hour = 5 }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.authwise_provider_magic_link.ml", "ttl", "15m"),
					resource.TestMatchResourceAttr("data.authwise_provider_magic_link.ml", "any",
						regexp.MustCompile(`"ttl":\s*"900s"`)),
					checkServer(func() error {
						ml, err := serverConfig()
						if err != nil {
							return err
						}
						if ml.GetTtl().AsDuration() != 15*time.Minute || ml.GetCodeLength() != 8 ||
							ml.GetMode() != corepb.ProviderMagicLink_LINK_AND_CODE ||
							ml.GetLinkScope() != corepb.ProviderMagicLink_ANY_BROWSER ||
							ml.GetUnknownAddress() != corepb.ProviderMagicLink_TELL {
							return fmt.Errorf("server config = %v", ml)
						}
						// One limit set alone; the others reach kit as 0, its default.
						if l := ml.GetRequestLimits(); l.GetPerIdentifierPerHour() != 5 || l.GetPerIpPerHour() != 0 || l.GetPerRealmPerHour() != 0 {
							return fmt.Errorf("request_limits = %v", l)
						}
						return nil
					}),
				),
			},
			{
				// A change inside the config is an in-place update of the
				// provider, not a replacement. link_scope, unknown_address and
				// request_limits left out reach kit unset, so kit's defaults
				// apply.
				Config: magicLink("5m", "CODE_ONLY", ""),
				Check: checkServer(func() error {
					ml, err := serverConfig()
					if err != nil {
						return err
					}
					if ml.GetTtl().AsDuration() != 5*time.Minute || ml.GetMode() != corepb.ProviderMagicLink_CODE_ONLY ||
						ml.GetLinkScope() != corepb.ProviderMagicLink_LINK_SCOPE_UNSPECIFIED ||
						ml.GetUnknownAddress() != corepb.ProviderMagicLink_UNKNOWN_ADDRESS_UNSPECIFIED ||
						ml.GetRequestLimits() != nil {
						return fmt.Errorf("server config after update = %v", ml)
					}
					return nil
				}),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccProviderMagicLink_Refusals pins the bounds the data source's
// validators do not know and kit enforces.
func TestAccProviderMagicLink_Refusals(t *testing.T) {

	type s struct {
		body string
		want *regexp.Regexp
	}

	cases := map[string]s{
		"code too long":         {body: `code_length = 9`, want: regexp.MustCompile(`code_length must be 6 to 8`)},
		"not matched on email":  {body: `identifier_attribute = "phone"`, want: regexp.MustCompile(`identifier_attribute must be email`)},
		"ttl is not a duration": {body: `ttl = "10"`, want: regexp.MustCompile(`invalid duration`)},
		"mode is not a mode":    {body: `mode = "LINK_ONLY"`, want: regexp.MustCompile(`value must be one of`)},
		// The enums take kit's spelling, as mode does.
		"link_scope in lower case":      {body: `link_scope = "any_browser"`, want: regexp.MustCompile(`value must be one of`)},
		"unknown_address in lower case": {body: `unknown_address = "tell"`, want: regexp.MustCompile(`value must be one of`)},
		// kit#831's bounds, refused at apply: there is no unlimited.
		"per identifier over 20": {body: `request_limits = { per_identifier_per_hour = 21 }`, want: regexp.MustCompile(`per_identifier_per_hour\s+must\s+be\s+1\s+to\s+20`)},
		"per ip negative":        {body: `request_limits = { per_ip_per_hour = -1 }`, want: regexp.MustCompile(`per_ip_per_hour\s+must\s+be\s+1\s+to\s+100`)},
		"per realm over 10000":   {body: `request_limits = { per_realm_per_hour = 10001 }`, want: regexp.MustCompile(`per_realm_per_hour\s+must\s+be\s+1\s+to\s+10000`)},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{
						Config: h.providerConfig() + `
data "authwise_provider_magic_link" "ml" {
  ` + v.body + `
}

resource "authwise_provider" "ml" {
  realm_id      = "r-1"
  provider_type = "magicLink"
  config        = data.authwise_provider_magic_link.ml.any
}
`,
						ExpectError: v.want,
					},
				},
			})
		})
	}
}

// TestAccProviderPasskey_Envelope: the passkey config has no fields, so the
// data source is worth exactly its any output — the type URL a plan would
// otherwise have to spell by hand.
func TestAccProviderPasskey_Envelope(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + realmForFactors + `
data "authwise_provider_passkey" "p" {}

resource "authwise_provider" "passkey" {
  realm_id      = local.realm_id
  display_name  = "Passkey"
  provider_type = "passkey"
  config        = data.authwise_provider_passkey.p.any
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.authwise_provider_passkey.p", "any",
						`{"@type":"type.googleapis.com/authwise.types.core.v1alpha1.ProviderPasskey"}`),
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						for _, p := range h.fake.providers {
							if !p.GetConfig().MessageIs(&corepb.ProviderPasskey{}) {
								return fmt.Errorf("provider config = %v", p.GetConfig())
							}
							return nil
						}
						return fmt.Errorf("no provider on the server")
					}),
				),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}
