// authwise_client says what a client may do (apis v0.21.0, kit#404):
// grant_types, kind, token_endpoint_auth_method, status and expires_at.
// kit derives the kind, method and status a config leaves out, so the
// computed values must land in state and plan clean afterwards.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const clientAddr = "authwise_client.app"

func TestAccClient_FactsDefaultAndUpdate(t *testing.T) {

	h := newHarness(t)

	config := func(extra string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_client" "app" {
  display_name = "App"
  grant_types  = ["authorization_code", "refresh_token"]
%s
}
`, extra)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// An application with nothing else said is public and active.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(clientAddr, "grant_types.#", "2"),
					resource.TestCheckResourceAttr(clientAddr, "grant_types.1", "refresh_token"),
					resource.TestCheckResourceAttr(clientAddr, "kind", "CLIENT_KIND_APPLICATION"),
					resource.TestCheckResourceAttr(clientAddr, "token_endpoint_auth_method", "none"),
					resource.TestCheckResourceAttr(clientAddr, "status", "CLIENT_STATUS_ACTIVE"),
					resource.TestCheckNoResourceAttr(clientAddr, "expires_at"),
				),
			},
			{
				// A confidential application that is switched off and expires,
				// changed in place.
				Config: config(`  token_endpoint_auth_method = "client_secret_post"
  status                     = "CLIENT_STATUS_DISABLED"
  expires_at                 = "2031-01-01T00:00:00Z"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(clientAddr, "token_endpoint_auth_method", "client_secret_post"),
					resource.TestCheckResourceAttr(clientAddr, "status", "CLIENT_STATUS_DISABLED"),
					resource.TestCheckResourceAttr(clientAddr, "expires_at", "2031-01-01T00:00:00Z"),
				),
			},
			{
				ResourceName:                         clientAddr,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateIdFunc:                    importByName(clientAddr),
			},
		},
	})
}

// A service registered for client_credentials alone is a SERVICE that
// authenticates with Basic, and can be given a secret.
func TestAccClient_ServiceDefaults(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.providerConfig() + `
resource "authwise_client" "app" {
  display_name = "CI"
  grant_types  = ["client_credentials"]
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(clientAddr, "kind", "CLIENT_KIND_SERVICE"),
				resource.TestCheckResourceAttr(clientAddr, "token_endpoint_auth_method", "client_secret_basic"),
			),
		}},
	})
}

// kit v1.38.0's refusals reach the apply.
func TestAccClient_Refusals(t *testing.T) {

	cases := map[string]struct {
		config string
		want   *regexp.Regexp
	}{
		"refresh without authorization_code": {
			config: `grant_types = ["refresh_token"]`,
			want:   regexp.MustCompile(`(?s)refresh_token\s+is\s+only\s+for\s+a\s+client\s+that\s+also\s+has`),
		},
		"public service": {
			config: `grant_types = ["client_credentials"]
  token_endpoint_auth_method = "none"`,
			want: regexp.MustCompile(`(?s)none\s+is\s+for\s+an\s+application`),
		},
		"saml_idp with an OAuth grant": {
			config: `grant_types = ["saml_idp", "authorization_code"]`,
			want:   regexp.MustCompile(`(?s)a\s+saml_idp\s+client\s+is\s+a\s+SAML\s+relying\s+party`),
		},
		"private_key_jwt": {
			config: `grant_types = ["client_credentials"]
  token_endpoint_auth_method = "private_key_jwt"`,
			want: regexp.MustCompile(`(?s)private_key_jwt\s+is\s+not\s+supported\s+yet`),
		},
		"no grants and no kind": {
			config: ``,
			want:   regexp.MustCompile(`(?s)kind:\s+""\s+is\s+not`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config: h.providerConfig() + fmt.Sprintf(`
resource "authwise_client" "app" {
  display_name = "App"
  %s
}
`, v.config),
					ExpectError: v.want,
				}},
			})
		})
	}
}

// A public application holds no secret: kit refuses to mint one.
func TestAccClient_PublicHoldsNoSecret(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.providerConfig() + `
resource "authwise_client" "app" {
  display_name = "SPA"
  grant_types  = ["authorization_code"]
}

resource "authwise_client_secret" "app" {
  client_id = authwise_client.app.client_id
}
`,
			ExpectError: regexp.MustCompile(`(?s)is\s+public\s+\(token_endpoint_auth_method\s+none\)`),
		}},
	})
}
