// SAML 2.0 acceptance coverage (kit#487): the certificate resource that
// holds the trust anchors, and the two config builder data sources that
// render kit's SP-role and IdP-role SAML configuration.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// partnerPEM is the certificate a partner hands over out of band. Its exact
// bytes matter: kit stores an imported certificate verbatim.
const partnerPEM = "-----BEGIN CERTIFICATE-----\npartner-signing\n-----END CERTIFICATE-----\n"

// onlyProvider returns the single provider on the fake server.
func onlyProvider(t *testing.T, m map[string]*corepb.Provider) *corepb.Provider {
	t.Helper()
	if len(m) != 1 {
		t.Fatalf("expected exactly one provider on the server, got %d", len(m))
	}
	for _, p := range m {
		return p
	}
	return nil
}

// onlyClient returns the single client on the fake server.
func onlyClient(t *testing.T, m map[string]*corepb.Client) *corepb.Client {
	t.Helper()
	if len(m) != 1 {
		t.Fatalf("expected exactly one client on the server, got %d", len(m))
	}
	for _, c := range m {
		return c
	}
	return nil
}

// TestAccCertificate_Mint covers the minting lane: the create-only
// parameters go up, the server answers with what it actually produced, and
// — the point of the exercise — the parameters stay in state afterwards.
//
// The implicit post-apply plan check is the regression test. The mint
// parameters are input-only and immutable; if a refresh nulled them, every
// plan would be non-empty and would force replacement, which is what the
// default Optional+Computed shape used to do.
func TestAccCertificate_Mint(t *testing.T) {

	h := newHarness(t)

	cert := func(displayName string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_certificate" "signing" {
  display_name        = %q
  use                 = "CERTIFICATE_USE_SIGNING"
  subject_common_name = "sp.example.com"
  validity_days       = 365
  key_size            = 2048

  labels = {
    role = "sp"
  }
}
`, displayName)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: cert("SP signing"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("authwise_certificate.signing", "name",
						regexp.MustCompile(`^tenants/t-1/certificates/cert-\d+$`)),

					// What the server derived from the parameters.
					resource.TestCheckResourceAttr("authwise_certificate.signing", "origin", "CERTIFICATE_ORIGIN_GENERATED"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "has_private_key", "true"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "subject", "CN=sp.example.com"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "not_before", "2026-09-14T00:00:00Z"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "not_after", "2027-09-14T00:00:00Z"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "status", "CERTIFICATE_STATUS_ACTIVE"),
					resource.TestCheckResourceAttrSet("authwise_certificate.signing", "key_id"),
					resource.TestCheckResourceAttrSet("authwise_certificate.signing", "certificate_pem"),

					// The parameters themselves survive in state.
					resource.TestCheckResourceAttr("authwise_certificate.signing", "subject_common_name", "sp.example.com"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "validity_days", "365"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "key_size", "2048"),

					checkServer(func() error {
						// They reached kit...
						if got := h.fake.lastMint.GetSubjectCommonName(); got != "sp.example.com" {
							return fmt.Errorf("server received subject_common_name = %q", got)
						}
						if got := h.fake.lastMint.GetValidityDays(); got != 365 {
							return fmt.Errorf("server received validity_days = %d", got)
						}
						if got := h.fake.lastMint.GetKeySize(); got != 2048 {
							return fmt.Errorf("server received key_size = %d", got)
						}
						// ...and the stored row kept none of them, so a read
						// can only ever answer with the zero value.
						for _, c := range h.fake.certs {
							if c.GetSubjectCommonName() != "" || c.GetValidityDays() != 0 || c.GetKeySize() != 0 {
								return fmt.Errorf("input-only fields were stored on the row: %v", c)
							}
						}
						return nil
					}),
				),
			},
			{
				// Renaming patches in place. An update rather than a replace
				// is the assertion: the immutable mint parameters must still
				// match state, which they only do because the refresh left
				// them alone.
				Config: cert("SP signing (rotated set)"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("authwise_certificate.signing", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_certificate.signing", "display_name", "SP signing (rotated set)"),
					resource.TestCheckResourceAttr("authwise_certificate.signing", "validity_days", "365"),
					checkServer(func() error {
						for _, c := range h.fake.certs {
							if c.GetLabels()["role"] != "sp" {
								return fmt.Errorf("untouched field changed: labels = %v", c.GetLabels())
							}
						}
						return nil
					}),
				),
			},
			{
				ResourceName: "authwise_certificate.signing",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_certificate.signing"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// The documented cost of input-only: kit never returns these,
				// so an imported resource cannot know them. Terraform plans a
				// replacement on the next apply unless the configuration
				// repeats what they were.
				ImportStateVerifyIgnore: []string{
					"subject_common_name",
					"validity_days",
					"key_size",
					"import_certificate_pem",
				},
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if n := len(h.fake.certs); n != 0 {
				return fmt.Errorf("%d certificates left on the server after destroy", n)
			}
			return nil
		},
	})
}

// TestAccCertificate_ImportPartnerPEM covers the other lane off the same
// field: a non-empty import_certificate_pem adds a partner's trust anchor
// instead of minting one. There is no ImportCertificate RPC.
func TestAccCertificate_ImportPartnerPEM(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + fmt.Sprintf(`
resource "authwise_certificate" "partner" {
  display_name           = "Partner signing"
  use                    = "CERTIFICATE_USE_SIGNING"
  import_certificate_pem = %q
}
`, partnerPEM)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_certificate.partner", "origin", "CERTIFICATE_ORIGIN_IMPORTED"),
					// An imported anchor is a public certificate and nothing more.
					resource.TestCheckResourceAttr("authwise_certificate.partner", "has_private_key", "false"),
					// certificate_pem is the read-only public certificate of
					// any row; import_certificate_pem is the write-only way in.
					resource.TestCheckResourceAttr("authwise_certificate.partner", "certificate_pem", partnerPEM),
					resource.TestCheckResourceAttr("authwise_certificate.partner", "import_certificate_pem", partnerPEM),
					checkServer(func() error {
						if got := h.fake.lastMint.GetImportCertificatePem(); got != partnerPEM {
							return fmt.Errorf("server received import_certificate_pem = %q", got)
						}
						for _, c := range h.fake.certs {
							if c.GetImportCertificatePem() != "" {
								return errStored(c.GetName())
							}
						}
						return nil
					}),
				),
			},
		},
	})
}

func errStored(name string) error {
	return fmt.Errorf("%s stored the input-only import_certificate_pem", name)
}

// TestAccProviderSaml_ClaimMap runs the SP role end to end: a claim map
// written as a nested block in HCL, through the config builder data source,
// packed as an Any onto a real provider resource, and unpacked on the
// server as a typed ProviderSaml.
func TestAccProviderSaml_ClaimMap(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_certificate" "sp_signing" {
  display_name        = "SP signing"
  use                 = "CERTIFICATE_USE_SIGNING_AND_ENCRYPTION"
  subject_common_name = "sp.example.com"
}

data "authwise_provider_saml" "partner" {
  idp_entity_id          = "https://idp.partner.example/metadata"
  idp_sso_url            = "https://idp.partner.example/sso"
  idp_sso_binding        = "HTTP-Redirect"
  signing_certificate_id = authwise_certificate.sp_signing.certificate_id
  sign_authn_requests    = true
  want_assertions_signed = true
  clock_skew_seconds     = 120
  identifier_source      = "assertion.name_id"

  claim_map = {
    map = {
      email      = "assertion.attributes.mail"
      given_name = "assertion.attributes.givenName"
    }
    static = {
      source = "partner-idp"
    }
    passthrough = ["department"]
  }
}

resource "authwise_provider" "partner" {
  realm_id      = "r-1"
  display_name  = "Partner IdP"
  provider_type = "saml"
  config        = data.authwise_provider_saml.partner.any
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// The nested block is a real object in state, not a JSON string.
					resource.TestCheckResourceAttr("data.authwise_provider_saml.partner",
						"claim_map.map.email", "assertion.attributes.mail"),
					resource.TestCheckResourceAttr("data.authwise_provider_saml.partner",
						"claim_map.static.source", "partner-idp"),
					resource.TestCheckResourceAttr("data.authwise_provider_saml.partner",
						"claim_map.passthrough.0", "department"),

					checkServer(func() error {

						p := onlyProvider(t, h.fake.providers)

						saml := &corepb.ProviderSaml{}
						if err := p.GetConfig().UnmarshalTo(saml); err != nil {
							return fmt.Errorf("provider config is not a ProviderSaml: %w", err)
						}

						if got := saml.GetIdpEntityId(); got != "https://idp.partner.example/metadata" {
							return fmt.Errorf("idp_entity_id = %q", got)
						}
						if !saml.GetSignAuthnRequests() || !saml.GetWantAssertionsSigned() {
							return fmt.Errorf("signing flags lost: %v", saml)
						}
						if got := saml.GetClockSkewSeconds(); got != 120 {
							return fmt.Errorf("clock_skew_seconds = %d", got)
						}
						if got := saml.GetSigningCertificateId(); got == "" {
							return fmt.Errorf("signing_certificate_id is empty; the certificate reference did not resolve")
						}

						cm := saml.GetClaimMap()
						if got := cm.GetMap()["email"]; got != "assertion.attributes.mail" {
							return fmt.Errorf("claim_map.map[email] = %q", got)
						}
						if got := cm.GetMap()["given_name"]; got != "assertion.attributes.givenName" {
							return fmt.Errorf("claim_map.map[given_name] = %q", got)
						}
						if got := cm.GetStatic()["source"]; got != "partner-idp" {
							return fmt.Errorf("claim_map.static[source] = %q", got)
						}
						if got := cm.GetPassthrough(); len(got) != 1 || got[0] != "department" {
							return fmt.Errorf("claim_map.passthrough = %v", got)
						}

						return nil
					}),
				),
			},
		},
	})
}

// TestAccSamlRelyingPartyConfig_Client runs the IdP role: kit issuing
// assertions to a relying party, configured on a real client resource.
func TestAccSamlRelyingPartyConfig_Client(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_certificate" "idp_signing" {
  display_name        = "IdP signing"
  use                 = "CERTIFICATE_USE_SIGNING"
  subject_common_name = "idp.example.com"
  validity_days       = 825
}

data "authwise_saml_relying_party_config" "app" {
  sp_entity_id               = "https://app.example.com/saml/metadata"
  acs_urls                   = ["https://app.example.com/saml/acs", "https://app.example.com/saml/acs2"]
  acs_binding                = "HTTP-POST"
  signing_certificate_id     = authwise_certificate.idp_signing.certificate_id
  sign_response              = true
  sign_assertions            = true
  assertion_lifetime_seconds = 300
  name_id_format             = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"

  claim_map = {
    map = {
      email = "user.email"
    }
    passthrough = ["groups"]
  }
}

resource "authwise_audience" "app" {
  display_name = "SAML app"
}

resource "authwise_client" "app" {
  display_name = "SAML app"
  audience_id  = authwise_audience.app.audience_id
  grant_types  = ["saml_idp"]
  config       = data.authwise_saml_relying_party_config.app.any
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.authwise_saml_relying_party_config.app",
						"acs_urls.#", "2"),
					resource.TestCheckResourceAttr("data.authwise_saml_relying_party_config.app",
						"claim_map.map.email", "user.email"),

					checkServer(func() error {

						c := onlyClient(t, h.fake.clients)

						rp := &corepb.SamlRelyingPartyConfig{}
						if err := c.GetConfig().UnmarshalTo(rp); err != nil {
							return fmt.Errorf("client config is not a SamlRelyingPartyConfig: %w", err)
						}

						if got := rp.GetSpEntityId(); got != "https://app.example.com/saml/metadata" {
							return fmt.Errorf("sp_entity_id = %q", got)
						}
						// Order matters: the first ACS is the default, used
						// for an IdP-initiated login and for a request that
						// names none.
						want := []string{"https://app.example.com/saml/acs", "https://app.example.com/saml/acs2"}
						got := rp.GetAcsUrls()
						if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
							return fmt.Errorf("acs_urls = %v, want %v", got, want)
						}
						if !rp.GetSignResponse() || !rp.GetSignAssertions() {
							return fmt.Errorf("signing flags lost: %v", rp)
						}
						if got := rp.GetAssertionLifetimeSeconds(); got != 300 {
							return fmt.Errorf("assertion_lifetime_seconds = %d", got)
						}
						if got := rp.GetClaimMap().GetMap()["email"]; got != "user.email" {
							return fmt.Errorf("claim_map.map[email] = %q", got)
						}
						if got := rp.GetClaimMap().GetPassthrough(); len(got) != 1 || got[0] != "groups" {
							return fmt.Errorf("claim_map.passthrough = %v", got)
						}
						return nil
					}),
				),
			},
		},
	})
}
