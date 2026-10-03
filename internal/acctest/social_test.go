// Social and enterprise providers (apis v0.8.0, kit#587, #20): the OAuth
// family's config data sources, with the client secret held by reference.
package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/protobuf/encoding/protojson"
)

const googleSecretMaterial = "google-client-secret-material"

// TestAccProviderGoogle_ThroughSecret is the path a practitioner takes: the
// client secret in an authwise_secret, the config built by the data source,
// the provider naming both. The material reaches kit through the secret and
// nowhere else — not the provider row, not its config, not state.
func TestAccProviderGoogle_ThroughSecret(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + realmForFactors + `
resource "authwise_secret" "google" {
  display_name = "Google client secret"
  payload_wo   = "` + googleSecretMaterial + `"
}

data "authwise_provider_google" "this" {
  client_id         = "123.apps.googleusercontent.com"
  client_secret_ref = { name = authwise_secret.google.name }
  scopes            = ["openid", "email", "profile"]
  hosted_domains    = ["example.com"]
  authorization_params = {
    prompt = "select_account"
  }
}

resource "authwise_provider" "google" {
  realm_id      = local.realm_id
  display_name  = "Sign in with Google"
  provider_type = "google"
  config        = data.authwise_provider_google.this.any
}
`

	noMaterialInState := func(s *terraform.State) error {
		for addr, r := range s.RootModule().Resources {
			for k, v := range r.Primary.Attributes {
				if strings.Contains(v, googleSecretMaterial) {
					return fmt.Errorf("%s.%s holds the secret material", addr, k)
				}
			}
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					noMaterialInState,
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						if len(h.fake.providers) != 1 {
							return fmt.Errorf("%d providers, want 1", len(h.fake.providers))
						}
						for _, p := range h.fake.providers {
							g := &corepb.ProviderGoogle{}
							if err := p.GetConfig().UnmarshalTo(g); err != nil {
								return err
							}
							if _, ok := h.fake.secrets[g.GetClientSecretRef().GetName()]; !ok {
								return fmt.Errorf("client_secret_ref %q names no secret", g.GetClientSecretRef().GetName())
							}
							if strings.Join(g.GetScopes(), " ") != "openid email profile" ||
								g.GetAuthorizationParams()["prompt"] != "select_account" ||
								len(g.GetHostedDomains()) != 1 {
								return fmt.Errorf("google config = %v", g)
							}
							if b, _ := protojson.Marshal(p); strings.Contains(string(b), googleSecretMaterial) {
								return fmt.Errorf("the provider row holds the secret material")
							}
						}
						for name := range h.fake.secrets {
							if got := h.fake.material[name]; len(got) != 1 || got[0] != googleSecretMaterial {
								return fmt.Errorf("secret material = %v", got)
							}
						}
						return nil
					}),
				),
			},
			{
				// A read after the apply: the refreshed state still carries
				// no material, and the plan is empty.
				RefreshState: true,
				Check:        noMaterialInState,
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccSocialConfigDataSources builds each new config and checks the
// packed Any carries the fields that make the type what it is.
func TestAccSocialConfigDataSources(t *testing.T) {

	type s struct {
		config string
		want   []*regexp.Regexp
	}

	cases := map[string]s{
		"apple: the .p8 key by reference": {
			config: `
data "authwise_provider_apple" "c" {
  client_id       = "com.example.web"
  team_id         = "TEAM123456"
  key_id          = "KEY1234567"
  private_key_ref = { name = "tenants/t-1/secrets/apple-p8" }
}`,
			want: []*regexp.Regexp{
				regexp.MustCompile(`"@type":\s*"type.googleapis.com/authwise.types.core.v1alpha1.ProviderApple"`),
				regexp.MustCompile(`"privateKeyRef":\s*\{\s*"name":\s*"tenants/t-1/secrets/apple-p8"`),
				regexp.MustCompile(`"teamId":\s*"TEAM123456"`),
			},
		},
		"oidc: discovery plus a pinned endpoint": {
			config: `
data "authwise_provider_oidc" "c" {
  issuer            = "https://example.okta.com/oauth2/default"
  client_id         = "okta-client"
  client_secret_ref = { name = "tenants/t-1/secrets/okta" }
  scopes            = ["openid", "email"]
  endpoints = {
    userinfo_url = "https://example.okta.com/oauth2/default/v1/userinfo"
  }
}`,
			want: []*regexp.Regexp{
				regexp.MustCompile(`"issuer":\s*"https://example.okta.com/oauth2/default"`),
				regexp.MustCompile(`"endpoints":\s*\{\s*"userinfoUrl"`),
				regexp.MustCompile(`"scopes":\s*\[\s*"openid",\s*"email"\s*\]`),
			},
		},
		"oauth: userinfo and a claim map": {
			config: `
data "authwise_provider_oauth" "c" {
  client_id         = "discord-client"
  client_secret_ref = { name = "tenants/t-1/secrets/discord" }
  authorization_url = "https://discord.com/oauth2/authorize"
  token_url         = "https://discord.com/api/oauth2/token"
  userinfo_url      = "https://discord.com/api/users/@me"
  scopes            = ["identify", "email"]
  identifier_source = "userinfo.id"
  claim_map = {
    map = { email = "userinfo.email", preferred_username = "userinfo.username" }
  }
}`,
			want: []*regexp.Regexp{
				regexp.MustCompile(`"identifierSource":\s*"userinfo.id"`),
				regexp.MustCompile(`"userinfoUrl":\s*"https://discord.com/api/users/@me"`),
				regexp.MustCompile(`"email":\s*"userinfo.email"`),
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			name := "data." + regexp.MustCompile(`data "(\w+)" "c"`).FindStringSubmatch(v.config)[1] + ".c"
			checks := make([]resource.TestCheckFunc, 0, len(v.want))
			for _, re := range v.want {
				checks = append(checks, resource.TestMatchResourceAttr(name, "any", re))
			}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{Config: h.providerConfig() + v.config, Check: resource.ComposeAggregateTestCheckFunc(checks...)},
				},
			})
		})
	}
}

// OAuth has no id_token to fall back on, so a config without an
// identifier_source or a claim map could sign nobody in; the data source
// refuses it before kit has to.
func TestAccProviderOAuth_RequiresIdentifierAndClaimMap(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.providerConfig() + `
data "authwise_provider_oauth" "c" {
  client_id = "discord-client"
}`,
				ExpectError: regexp.MustCompile(`(?s)argument "(identifier_source|claim_map)" is required`),
			},
		},
	})
}

// TestAccSocialProvider_Refusals pins kit's write-time rules for the OAuth
// family: authorization_params cannot override a parameter kit sets, and an
// oauth provider's identifier must come from the userinfo document.
// TestAccProvider_LinkByVerifiedEmail is the per-provider opt-in to the
// login-time link by verified email (kit#633, apis v0.13.0): set on create,
// cleared in place, and reaching kit's row each time.
func TestAccProvider_LinkByVerifiedEmail(t *testing.T) {

	h := newHarness(t)

	config := func(link bool) string {
		return h.providerConfig() + realmForFactors + fmt.Sprintf(`
data "authwise_provider_google" "this" {
  client_id = "123.apps.googleusercontent.com"
}

resource "authwise_provider" "google" {
  realm_id               = local.realm_id
  provider_type          = "google"
  config                 = data.authwise_provider_google.this.any
  link_by_verified_email = %t
}
`, link)
	}

	serverHas := func(want bool) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			for _, p := range h.fake.providers {
				if p.GetLinkByVerifiedEmail() != want {
					return fmt.Errorf("link_by_verified_email = %t on the row, want %t", p.GetLinkByVerifiedEmail(), want)
				}
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_provider.google", "link_by_verified_email", "true"),
					serverHas(true),
				),
			},
			{
				Config: config(false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_provider.google", "link_by_verified_email", "false"),
					serverHas(false),
				),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccProvider_TrustUpstreamEmailVerified is the per-provider opt-in to
// counting the upstream's verified address as kit's own proof (kit#663,
// apis v0.16.0): set on create, cleared in place, and reaching kit's row
// each time.
func TestAccProvider_TrustUpstreamEmailVerified(t *testing.T) {

	h := newHarness(t)

	config := func(trust bool) string {
		return h.providerConfig() + realmForFactors + fmt.Sprintf(`
data "authwise_provider_google" "this" {
  client_id = "123.apps.googleusercontent.com"
}

resource "authwise_provider" "google" {
  realm_id                      = local.realm_id
  provider_type                 = "google"
  config                        = data.authwise_provider_google.this.any
  trust_upstream_email_verified = %t
}
`, trust)
	}

	serverHas := func(want bool) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			for _, p := range h.fake.providers {
				if p.GetTrustUpstreamEmailVerified() != want {
					return fmt.Errorf("trust_upstream_email_verified = %t on the row, want %t", p.GetTrustUpstreamEmailVerified(), want)
				}
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_provider.google", "trust_upstream_email_verified", "true"),
					serverHas(true),
				),
			},
			{
				Config: config(false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_provider.google", "trust_upstream_email_verified", "false"),
					serverHas(false),
				),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

func TestAccSocialProvider_Refusals(t *testing.T) {

	type s struct {
		config string
		want   *regexp.Regexp
	}

	cases := map[string]s{
		"google overriding scope through authorization_params": {
			config: `
data "authwise_provider_google" "c" {
  client_id            = "g"
  authorization_params = { Scope = "openid" }
}

resource "authwise_provider" "p" {
  realm_id      = "r-1"
  provider_type = "google"
  config        = data.authwise_provider_google.c.any
}`,
			want: regexp.MustCompile(`(?s)authorization_params may not set\s+"Scope"`),
		},
		"oauth identifier outside userinfo": {
			config: `
data "authwise_provider_oauth" "c" {
  identifier_source = "id_token.sub"
  claim_map         = { map = { email = "userinfo.email" } }
}

resource "authwise_provider" "p" {
  realm_id      = "r-1"
  provider_type = "oauth"
  config        = data.authwise_provider_oauth.c.any
}`,
			want: regexp.MustCompile(`(?s)identifier_source must be\s+userinfo`),
		},
		"a config of another provider's type": {
			config: `
data "authwise_provider_github" "c" {
  client_id = "gh"
}

resource "authwise_provider" "p" {
  realm_id      = "r-1"
  provider_type = "google"
  config        = data.authwise_provider_github.c.any
}`,
			want: regexp.MustCompile(`(?s)a google provider takes a\s+ProviderGoogle config`),
		},
		"link_by_verified_email on a type with no upstream": {
			config: `
data "authwise_provider_magic_link" "c" {}

resource "authwise_provider" "p" {
  realm_id               = "r-1"
  provider_type          = "magicLink"
  config                 = data.authwise_provider_magic_link.c.any
  link_by_verified_email = true
}`,
			want: regexp.MustCompile(`(?s)link_by_verified_email is for a\s+federated\s+provider; magicLink is not one`),
		},
		"trust_upstream_email_verified on a type with no upstream": {
			config: `
data "authwise_provider_magic_link" "c" {}

resource "authwise_provider" "p" {
  realm_id                      = "r-1"
  provider_type                 = "magicLink"
  config                        = data.authwise_provider_magic_link.c.any
  trust_upstream_email_verified = true
}`,
			want: regexp.MustCompile(`(?s)trust_upstream_email_verified is for\s+a\s+federated\s+provider; magicLink is not\s+one`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{Config: h.providerConfig() + v.config, ExpectError: v.want},
				},
			})
		})
	}
}
