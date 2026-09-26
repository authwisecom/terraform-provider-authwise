// Authn + secrets acceptance coverage (kit#544): realm factors, the realm
// authentication policy, authwise_secret with its write-only material, and
// the references that name a secret instead of holding it.
package acctest_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
	"time"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/protobuf/proto"
)

// realmForFactors is the realm every factor test hangs off; realm_id is its
// last name segment, since the provider block sets no realm default.
const realmForFactors = `
resource "authwise_realm" "r" {
  display_name = "Employees"
}

locals {
  realm_id = element(split("/", authwise_realm.r.name), 3)
}
`

// factorConfig unpacks the single factor's Any config into want's type.
func factorConfig(h *harness, want proto.Message) error {
	fa := h.fake.onlyFactor()
	if fa == nil {
		return fmt.Errorf("no factor on the server")
	}
	if err := fa.GetConfig().UnmarshalTo(want); err != nil {
		return fmt.Errorf("factor config: %w", err)
	}
	return nil
}

func noneLeft(h *harness) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		if n := len(h.fake.factors) + len(h.fake.secrets) + len(h.fake.endpoints) + len(h.fake.realms); n != 0 {
			return fmt.Errorf("%d objects left on the server after destroy", n)
		}
		return nil
	}
}

func TestAccFactor_TOTP(t *testing.T) {

	h := newHarness(t)

	factor := func(status string) string {
		return h.providerConfig() + realmForFactors + fmt.Sprintf(`
data "authwise_factor_totp" "totp" {
  digits    = 8
  algorithm = "SHA256"
}

resource "authwise_factor" "totp" {
  realm_id     = local.realm_id
  display_name = "Authenticator app"
  factor_type  = "totp"
  config       = data.authwise_factor_totp.totp.any
  status       = %q
}
`, status)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: factor("active"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("authwise_factor.totp", "name",
						regexp.MustCompile(`^tenants/t-1/realms/r-\d+/factors/f-\d+$`)),
					resource.TestCheckResourceAttr("authwise_factor.totp", "factor_type", "totp"),
					checkServer(func() error {
						var cfg corepb.FactorTOTP
						if err := factorConfig(h, &cfg); err != nil {
							return err
						}
						if cfg.GetDigits() != 8 || cfg.GetAlgorithm() != "SHA256" {
							return fmt.Errorf("server config = %v", &cfg)
						}
						return nil
					}),
				),
			},
			{
				// Disabling is a patch of status, not a delete: enrolled
				// authenticators survive it.
				Config: factor("disabled"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_factor.totp", plancheck.ResourceActionUpdate),
					},
				},
				Check: checkServer(func() error {
					if s := h.fake.onlyFactor().GetStatus(); s != "disabled" {
						return fmt.Errorf("server status = %q", s)
					}
					return nil
				}),
			},
			{
				ResourceName: "authwise_factor.totp",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_factor.totp"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// Scope identifiers are inputs the import ID does not carry.
				ImportStateVerifyIgnore: []string{"realm_id"},
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

func TestAccFactor_WebAuthn(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + realmForFactors + `
data "authwise_factor_webauthn" "passkeys" {
  rp_id             = "login.example.com"
  rp_display_name   = "Example"
  allowed_origins   = ["https://app.example.com"]
  user_verification = "required"
}

resource "authwise_factor" "webauthn" {
  realm_id     = local.realm_id
  display_name = "Passkeys"
  factor_type  = "webauthn"
  config       = data.authwise_factor_webauthn.passkeys.any
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// kit defaults a new factor to active.
					resource.TestCheckResourceAttr("authwise_factor.webauthn", "status", "active"),
					checkServer(func() error {
						var cfg corepb.FactorWebAuthn
						if err := factorConfig(h, &cfg); err != nil {
							return err
						}
						if cfg.GetRpId() != "login.example.com" || cfg.GetUserVerification() != "required" ||
							fmt.Sprint(cfg.GetAllowedOrigins()) != "[https://app.example.com]" {
							return fmt.Errorf("server config = %v", &cfg)
						}
						return nil
					}),
				),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccFactor_DuoReferencesSecret wires a Duo factor to its client secret
// by reference. The destroy is the other half of the test: kit refuses to
// delete a referenced secret, so it only succeeds if the reference's
// implicit dependency ordered the factor's destruction first.
func TestAccFactor_DuoReferencesSecret(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + realmForFactors + `
resource "authwise_secret" "duo" {
  display_name = "Duo client secret"
  payload_wo   = "duo-secret-material"
}

data "authwise_factor_duo" "duo" {
  client_id = "DIXXXXXXXXXXXXXXXXXX"
  api_host  = "api-12345678.duosecurity.com"
  client_secret_ref = {
    name = authwise_secret.duo.name
  }
}

resource "authwise_factor" "duo" {
  realm_id     = local.realm_id
  display_name = "Duo"
  factor_type  = "duo"
  config       = data.authwise_factor_duo.duo.any
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: checkServer(func() error {
					var cfg corepb.FactorDuo
					if err := factorConfig(h, &cfg); err != nil {
						return err
					}
					if got, want := cfg.GetClientSecretRef().GetName(), h.fake.onlySecretName(); got != want {
						return fmt.Errorf("client_secret_ref = %q, want %q", got, want)
					}
					return nil
				}),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccEndpoint_BearerAuth covers the endpoint's auth oneof on the JSON
// lane: a reference inside jsonencode still orders the endpoint after the
// secret it names, on create and on destroy.
func TestAccEndpoint_BearerAuth(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_secret" "token" {
  display_name = "Risk service token"
  payload_wo   = "bearer-token-material"
}

resource "authwise_endpoint" "risk" {
  display_name = "Risk service"
  address      = "https://risk.example.com/v1"
  auth = jsonencode({
    bearer = {
      token = { name = authwise_secret.token.name }
    }
  })
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: checkServer(func() error {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					for _, e := range h.fake.endpoints {
						for name := range h.fake.secrets {
							if got := e.GetAuth().GetBearer().GetToken().GetName(); got != name {
								return fmt.Errorf("bearer token ref = %q, want %q", got, name)
							}
						}
						return nil
					}
					return fmt.Errorf("no endpoint on the server")
				}),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// onlyEndpoint returns the single endpoint on the fake server.
func onlyEndpoint(h *harness) (*corepb.Endpoint, error) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if len(h.fake.endpoints) != 1 {
		return nil, fmt.Errorf("%d endpoints on the server, want 1", len(h.fake.endpoints))
	}
	for _, e := range h.fake.endpoints {
		return proto.Clone(e).(*corepb.Endpoint), nil
	}
	return nil, nil
}

// TestAccEndpoint_TLSTimeoutAndAuthScheme is the endpoint's whole typed
// surface over one lifecycle: tls and timeout go in and read back intact,
// the auth scheme changes from a stored bearer token to a token kit signs,
// and an import reads the same thing back.
func TestAccEndpoint_TLSTimeoutAndAuthScheme(t *testing.T) {

	h := newHarness(t)

	endpoint := func(timeout, auth string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_secret" "token" {
  display_name = "Ledger token"
  payload_wo   = "bearer-token-material"
}

resource "authwise_issuer" "internal" {
  domain_name = "auth.example.com"
  path        = "/internal"
}

resource "authwise_certificate" "kit_client" {
  display_name        = "kit client"
  use                 = "CERTIFICATE_USE_SIGNING"
  subject_common_name = "kit.example.com"
}

resource "authwise_endpoint" "ledger" {
  display_name  = "Ledger"
  endpoint_type = "GRPC"
  address       = "ledger.example.com:443"
  timeout       = %q
  tls = {
    server_name        = "ledger.internal"
    client_certificate = authwise_certificate.kit_client.name
  }
  auth = jsonencode(%s)
}
`, timeout, auth)
	}

	const bearer = `{ bearer = { token = { name = authwise_secret.token.name } } }`
	const kitToken = `{ kitToken = { issuer = authwise_issuer.internal.name, audience = "https://ledger.example.com" } }`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// "2500ms" is stored as 2.5 s and still reads back as written.
				Config: endpoint("2500ms", bearer),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_endpoint.ledger", "timeout", "2500ms"),
					resource.TestCheckResourceAttr("authwise_endpoint.ledger", "tls.server_name", "ledger.internal"),
					resource.TestCheckResourceAttrPair("authwise_endpoint.ledger", "tls.client_certificate",
						"authwise_certificate.kit_client", "name"),
					resource.TestCheckResourceAttr("authwise_endpoint.ledger", "tls.insecure_skip_verify", "false"),
					checkServer(func() error {
						e, err := onlyEndpoint(h)
						if err != nil {
							return err
						}
						if got := e.GetTimeout().AsDuration(); got != 2500*time.Millisecond {
							return fmt.Errorf("server timeout = %s, want 2.5s", got)
						}
						if e.GetTls().GetServerName() != "ledger.internal" || e.GetTls().GetClientCertificate() == "" {
							return fmt.Errorf("server tls = %v", e.GetTls())
						}
						if e.GetAuth().GetBearer() == nil {
							return fmt.Errorf("server auth = %v, want bearer", e.GetAuth())
						}
						return nil
					}),
				),
			},
			{
				// Swapping the scheme and the deadline patches exactly those
				// two paths; tls is untouched.
				Config: endpoint("10s", kitToken),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_endpoint.ledger", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_endpoint.ledger", "timeout", "10s"),
					checkServer(func() error {
						e, err := onlyEndpoint(h)
						if err != nil {
							return err
						}
						kt := e.GetAuth().GetKitToken()
						if kt == nil || kt.GetAudience() != "https://ledger.example.com" || e.GetAuth().GetBearer() != nil {
							return fmt.Errorf("server auth = %v, want kit_token alone", e.GetAuth())
						}
						if got := e.GetTimeout().AsDuration(); got != 10*time.Second {
							return fmt.Errorf("server timeout = %s, want 10s", got)
						}
						if e.GetTls().GetServerName() != "ledger.internal" {
							return fmt.Errorf("tls changed on an auth update: %v", e.GetTls())
						}
						return nil
					}),
				),
			},
			{
				ResourceName: "authwise_endpoint.ledger",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_endpoint.ledger"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// An import has no configured spelling of auth to keep, so it
				// reads protojson's key order; the verify compares strings,
				// so auth is compared decoded instead.
				ImportStateVerifyIgnore: []string{"auth"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					var auth struct {
						KitToken struct {
							Issuer   string `json:"issuer"`
							Audience string `json:"audience"`
						} `json:"kitToken"`
					}
					if len(states) != 1 {
						return fmt.Errorf("%d imported instances, want 1", len(states))
					}
					if err := json.Unmarshal([]byte(states[0].Attributes["auth"]), &auth); err != nil {
						return fmt.Errorf("imported auth: %w", err)
					}
					if auth.KitToken.Audience != "https://ledger.example.com" || auth.KitToken.Issuer == "" {
						return fmt.Errorf("imported auth = %s", states[0].Attributes["auth"])
					}
					if got := states[0].Attributes["timeout"]; got != "10s" {
						return fmt.Errorf("imported timeout = %q, want 10s", got)
					}
					return nil
				},
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccEndpoint_KitRefusals pins the writes kit refuses, so the
// documentation's claims about them stay true: insecure means plaintext and
// exists only for gRPC, tls needs a TLS connection to describe, and the
// timeout has bounds.
func TestAccEndpoint_KitRefusals(t *testing.T) {

	type s struct {
		body string
		want *regexp.Regexp
	}

	cases := map[string]s{
		"insecure on REST": {
			body: `
  address  = "http://risk.example.com"
  insecure = true`,
			want: regexp.MustCompile(`insecure is refused`),
		},
		"tls on an insecure gRPC endpoint": {
			body: `
  endpoint_type = "GRPC"
  address       = "risk.example.com:50051"
  insecure      = true
  tls           = { server_name = "risk.internal" }`,
			want: regexp.MustCompile(`tls is refused`),
		},
		"timeout past 60s": {
			body: `
  address = "https://risk.example.com"
  timeout = "2m"`,
			want: regexp.MustCompile(`timeout must be between`),
		},
		"timeout that is not a duration": {
			body: `
  address = "https://risk.example.com"
  timeout = "5"`,
			want: regexp.MustCompile(`invalid duration`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{
					{
						Config:      h.providerConfig() + "resource \"authwise_endpoint\" \"e\" {" + v.body + "\n}\n",
						ExpectError: v.want,
					},
				},
			})
		})
	}
}

// TestAccSecret_Lifecycle pins where the material goes: to the server on
// create and on each rotation, and nowhere in state.
func TestAccSecret_Lifecycle(t *testing.T) {

	h := newHarness(t)

	secret := func(displayName, payload, version string) string {
		v := ""
		if version != "" {
			v = "payload_wo_version = " + version
		}
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_secret" "s" {
  display_name = %q
  description  = "Upstream API key"
  labels = {
    team = "platform"
  }
  payload_wo = %q
  %s
}
`, displayName, payload, v)
	}

	wantMaterial := func(want ...string) resource.TestCheckFunc {
		return checkServer(func() error {
			if got := h.fake.secretMaterial(h.fake.onlySecretName()); fmt.Sprint(got) != fmt.Sprint(want) {
				return fmt.Errorf("server material = %v, want %v", got, want)
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: secret("API key", "v1-material", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("authwise_secret.s", "name",
						regexp.MustCompile(`^tenants/t-1/secrets/s-\d+$`)),
					resource.TestCheckResourceAttr("authwise_secret.s", "version", "1"),
					resource.TestCheckResourceAttr("authwise_secret.s", "updated_at", certNotBefore.Format(time.RFC3339)),
					resource.TestCheckNoResourceAttr("authwise_secret.s", "payload_wo"),
					wantMaterial("v1-material"),
				),
			},
			{
				// New material alone is invisible to Terraform — nothing to
				// diff it against — so nothing is sent.
				Config: secret("API key", "v2-material", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: wantMaterial("v1-material"),
			},
			{
				// Setting the version is the rotation signal.
				Config: secret("API key", "v2-material", "2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_secret.s", "version", "2"),
					resource.TestCheckNoResourceAttr("authwise_secret.s", "payload_wo"),
					wantMaterial("v1-material", "v2-material"),
				),
			},
			{
				// Metadata patches without touching the material.
				Config: secret("Upstream key", "v2-material", "2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_secret.s", "display_name", "Upstream key"),
					resource.TestCheckResourceAttr("authwise_secret.s", "version", "2"),
					wantMaterial("v1-material", "v2-material"),
				),
			},
			{
				ResourceName: "authwise_secret.s",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_secret.s"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// payload_wo_version is the practitioner's rotation counter;
				// nothing on the server records it.
				ImportStateVerifyIgnore: []string{"payload_wo_version"},
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

func TestAccSecret_ExternalRefusesPayload(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_secret" "s" {
  external = {
    store = "vault"
    key   = "upstream"
  }
  payload_wo = "material"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`external secrets take no material`),
			},
		},
	})
}

// TestAccSecret_ReferencedDeleteRefused removes a secret that something
// outside the configuration still references: kit refuses, and the provider
// says what to do about it.
func TestAccSecret_ReferencedDeleteRefused(t *testing.T) {

	h := newHarness(t)

	withSecret := h.providerConfig() + `
resource "authwise_secret" "s" {
  payload_wo = "material"
}
`
	without := h.providerConfig()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: withSecret,
			},
			{
				PreConfig: func() {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					for name := range h.fake.secrets {
						h.fake.pinned[name] = true
					}
				},
				Config:      without,
				ExpectError: regexp.MustCompile(`(?s)the secret is still referenced.*is referenced by 1 object`),
			},
			{
				// Once the reference is gone the same removal goes through.
				PreConfig: func() {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					h.fake.pinned = map[string]bool{}
				},
				Config: without,
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccRealmAuthenticationPolicy_Lifecycle covers the policy resource and
// its truce with authwise_realm: each owns its part of RealmConfig, and a
// change to either leaves the other's alone.
func TestAccRealmAuthenticationPolicy_Lifecycle(t *testing.T) {

	h := newHarness(t)

	config := func(locale, policy string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "r" {
  display_name = "Employees"
  config       = jsonencode({ defaultLocale = %q })
}

resource "authwise_realm_authentication_policy" "p" {
  realm = authwise_realm.r.name
%s
}
`, locale, policy)
	}

	const policy = `
  rules = [
    {
      name      = "admins"
      condition = "user.groups.exists(g, g == 'admins')"
      require = {
        mode         = "PHISHING_RESISTANT"
        reauth_after = "15m"
      }
    },
    {
      name = "default"
      require = {
        mode                 = "ANY_FACTOR"
        allowed_factor_types = ["totp", "webauthn"]
      }
    },
  ]
  floor = {
    mode = "ANY_FACTOR"
  }
  enrollment = {
    in_flow            = true
    grace              = "72h"
    self_service_types = ["totp", "webauthn"]
  }
  session = {
    absolute      = "8h"
    factor_reauth = "12h"
  }
  risk = {
    mode    = "BUILTIN"
    weights = { new_device = 30 }
  }
  acr_levels = [
    { name = "mfa", require = { mode = "ANY_FACTOR" } },
  ]
`

	// explicitZeros restates defaults explicitly. proto3 reads them back as
	// unset, which must not show as drift.
	const explicitZeros = `
  floor = {
    mode                   = "ANY_FACTOR"
    min_factors            = 0
    skip_if_device_trusted = false
  }
  enrollment = {
    in_flow = false
  }
`

	realm := func() *corepb.Realm {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		return proto.Clone(onlyRealm(t, h.fake.realms)).(*corepb.Realm)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("fr", policy),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_realm_authentication_policy.p", "rules.#", "2"),
					// The realm resource does not see the policy.
					resource.TestCheckResourceAttr("authwise_realm.r", "config", `{"defaultLocale":"fr"}`),
					checkServer(func() error {
						c := realm().GetConfig()
						a := c.GetAuthentication()
						if c.GetDefaultLocale() != "fr" {
							return fmt.Errorf("default_locale = %q", c.GetDefaultLocale())
						}
						if len(a.GetRules()) != 2 || a.GetRules()[0].GetRequire().GetMode() != corepb.Requirement_PHISHING_RESISTANT {
							return fmt.Errorf("rules = %v", a.GetRules())
						}
						if d := a.GetRules()[0].GetRequire().GetReauthAfter().AsDuration(); d != 15*time.Minute {
							return fmt.Errorf("reauth_after = %s", d)
						}
						if a.GetRisk().GetWeights()["new_device"] != 30 {
							return fmt.Errorf("risk = %v", a.GetRisk())
						}
						return nil
					}),
				),
			},
			{
				// A realm config change keeps the policy it would otherwise
				// overwrite.
				Config: config("de", policy),
				Check: checkServer(func() error {
					c := realm().GetConfig()
					if c.GetDefaultLocale() != "de" {
						return fmt.Errorf("default_locale = %q", c.GetDefaultLocale())
					}
					if len(c.GetAuthentication().GetRules()) != 2 {
						return fmt.Errorf("the realm update erased the policy: %v", c.GetAuthentication())
					}
					return nil
				}),
			},
			{
				Config: config("de", explicitZeros),
				Check: checkServer(func() error {
					c := realm().GetConfig()
					if c.GetDefaultLocale() != "de" {
						return fmt.Errorf("the policy update touched the realm config: %v", c)
					}
					if a := c.GetAuthentication(); len(a.GetRules()) != 0 || a.GetEnrollment() == nil || a.GetEnrollment().GetInFlow() {
						return fmt.Errorf("policy = %v", a)
					}
					return nil
				}),
			},
			{
				ResourceName: "authwise_realm_authentication_policy.p",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_realm.r"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "realm",
				// Explicit zeros are indistinguishable from unset on the
				// server, so an import reads them as null.
				ImportStateVerifyIgnore: []string{
					"floor.min_factors", "floor.skip_if_device_trusted", "enrollment.in_flow",
				},
			},
			{
				// Destroying the policy clears it and nothing else.
				Config: h.providerConfig() + `
resource "authwise_realm" "r" {
  display_name = "Employees"
  config       = jsonencode({ defaultLocale = "de" })
}
`,
				Check: checkServer(func() error {
					c := realm().GetConfig()
					if c.GetAuthentication() != nil || c.GetDefaultLocale() != "de" {
						return fmt.Errorf("config after policy destroy = %v", c)
					}
					return nil
				}),
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

func TestAccRealm_RefusesAuthenticationInConfig(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_realm" "r" {
  display_name = "Employees"
  config       = jsonencode({ authentication = { floor = { mode = "ANY_FACTOR" } } })
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`the authentication policy is managed separately`),
			},
		},
	})
}
