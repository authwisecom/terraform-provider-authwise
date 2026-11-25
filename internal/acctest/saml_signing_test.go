package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"google.golang.org/protobuf/proto"
)

// The SAML signing switches are unset, true or false (tf#89, K-34,
// kit#1041, apis v0.91.0): want_assertions_signed on a SAML provider,
// sign_response and sign_assertions on a relying party. Unset reads as
// true on kit v1.100.0, so an attribute the configuration omits must reach
// kit absent; an explicit false must reach it as false, where kit refuses
// it unless the other signature is named. Every applied step also plans
// clean after refresh, so the config kit returns matches the one written.

// optionalBool compares a switch as kit holds it (nil when absent) with
// want (nil for absent).
func optionalBool(field string, got, want *bool) error {
	switch {
	case want == nil && got != nil:
		return fmt.Errorf("%s reached kit as %t; an omitted attribute must stay absent", field, *got)
	case want != nil && got == nil:
		return fmt.Errorf("%s is absent on the server; want %t", field, *want)
	case want != nil && *got != *want:
		return fmt.Errorf("%s = %t on the server; want %t", field, *got, *want)
	}
	return nil
}

func TestAccProviderSaml_WantAssertionsSigned(t *testing.T) {

	h := newHarness(t)
	const ds = "data.authwise_provider_saml.partner"

	config := func(lines string) string {
		return h.providerConfig() + fmt.Sprintf(`
data "authwise_provider_saml" "partner" {
  idp_entity_id = "https://idp.partner.example/metadata"
  idp_sso_url   = "https://idp.partner.example/sso"
  %s
}

resource "authwise_provider" "partner" {
  realm_id      = "r-1"
  display_name  = "Partner IdP"
  provider_type = "saml"
  config        = data.authwise_provider_saml.partner.any
}
`, lines)
	}

	// want checks the stored config: want_assertions_signed and
	// want_response_signed.
	want := func(assertions *bool, response bool) resource.TestCheckFunc {
		return checkServer(func() error {
			p := func() *corepb.Provider {
				h.fake.mu.Lock()
				defer h.fake.mu.Unlock()
				return proto.Clone(onlyProvider(t, h.fake.providers)).(*corepb.Provider)
			}()
			saml := &corepb.ProviderSaml{}
			if err := p.GetConfig().UnmarshalTo(saml); err != nil {
				return fmt.Errorf("provider config is not a ProviderSaml: %w", err)
			}
			if err := optionalBool("want_assertions_signed", saml.WantAssertionsSigned, assertions); err != nil {
				return err
			}
			if saml.GetWantResponseSigned() != response {
				return fmt.Errorf("want_response_signed = %t; want %t", saml.GetWantResponseSigned(), response)
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Omitted: absent on the server, which kit reads as signed.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(nil, false),
					resource.TestCheckNoResourceAttr(ds, "want_assertions_signed"),
				),
			},
			{
				Config: config(`want_assertions_signed = true`),
				Check:  want(proto.Bool(true), false),
			},
			{
				// A lone false is sent as false, and kit refuses it.
				Config:      config(`want_assertions_signed = false`),
				ExpectError: regexp.MustCompile(`want_assertions_signed: false would\s+leave\s+the\s+assertion\s+unverified`),
			},
			{
				// Beside a signed Response, kit takes it.
				Config: config(`
  want_assertions_signed = false
  want_response_signed   = true
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(proto.Bool(false), true),
					resource.TestCheckResourceAttr(ds, "want_assertions_signed", "false"),
				),
			},
			{
				// Removing the line sends it absent again: signed.
				Config: config(`want_response_signed = true`),
				Check:  want(nil, true),
			},
		},
	})
}

func TestAccSamlRelyingPartyConfig_Signing(t *testing.T) {

	h := newHarness(t)
	const ds = "data.authwise_saml_relying_party_config.app"

	config := func(lines string) string {
		return h.providerConfig() + fmt.Sprintf(`
data "authwise_saml_relying_party_config" "app" {
  sp_entity_id = "https://app.example.com/saml/metadata"
  acs_urls     = ["https://app.example.com/saml/acs"]
  %s
}

resource "authwise_audience" "app" {
  display_name = "SAML app"
}

resource "authwise_application" "app" {
  display_name = "SAML app"
  audience_id  = authwise_audience.app.audience_id
  grant_types  = ["saml_idp"]
  config       = data.authwise_saml_relying_party_config.app.any
}
`, lines)
	}

	want := func(response, assertions *bool) resource.TestCheckFunc {
		return checkServer(func() error {
			c := func() *corepb.Application {
				h.fake.mu.Lock()
				defer h.fake.mu.Unlock()
				return proto.Clone(onlyClient(t, h.fake.clients)).(*corepb.Application)
			}()
			rp := &corepb.SamlRelyingPartyConfig{}
			if err := c.GetConfig().UnmarshalTo(rp); err != nil {
				return fmt.Errorf("client config is not a SamlRelyingPartyConfig: %w", err)
			}
			if err := optionalBool("sign_response", rp.SignResponse, response); err != nil {
				return err
			}
			return optionalBool("sign_assertions", rp.SignAssertions, assertions)
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Omitted: both absent, which kit reads as both signed.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(nil, nil),
					resource.TestCheckNoResourceAttr(ds, "sign_response"),
					resource.TestCheckNoResourceAttr(ds, "sign_assertions"),
				),
			},
			{
				Config: config(`
  sign_response   = true
  sign_assertions = true
`),
				Check: want(proto.Bool(true), proto.Bool(true)),
			},
			{
				// A lone sign_response = false is refused: the default
				// true of sign_assertions does not stand in for it.
				Config:      config(`sign_response = false`),
				ExpectError: regexp.MustCompile(`sign_response: false would\s+leave\s+the\s+Response\s+unsigned\s+and\s+sign_assertions\s+is\s+not\s+set`),
			},
			{
				Config: config(`
  sign_response   = false
  sign_assertions = true
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(proto.Bool(false), proto.Bool(true)),
					resource.TestCheckResourceAttr(ds, "sign_response", "false"),
				),
			},
			{
				// sign_assertions = false beside a Response signed by
				// default is accepted.
				Config: config(`sign_assertions = false`),
				Check: resource.ComposeAggregateTestCheckFunc(
					want(nil, proto.Bool(false)),
					resource.TestCheckResourceAttr(ds, "sign_assertions", "false"),
				),
			},
			{
				Config: config(""),
				Check:  want(nil, nil),
			},
		},
	})
}
