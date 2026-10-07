// authwise_client_secret (kit#617, #18): minted through MintClientSecret,
// its secret returned once and kept in state from there on. The steps walk
// what that has to survive — a refresh, an in-place expires_at change, a
// keepers-driven rotation — and what it cannot: an import has no secret.
package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const clientSecretAddr = "authwise_client_secret.ci"

func TestAccClientSecret_MintKeepRotate(t *testing.T) {

	h := newHarness(t)

	config := func(expires, rotation string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_client" "ci" {
  display_name = "CI"
  grant_types  = ["client_credentials"]
}

resource "authwise_client_secret" "ci" {
  client_id  = authwise_client.ci.client_id
  expires_at = %q
  keepers    = { rotation = %q }

  lifecycle {
    create_before_destroy = true
  }
}

data "authwise_client_secrets" "ci" {
  client_id  = authwise_client.ci.client_id
  depends_on = [authwise_client_secret.ci]
}
`, expires, rotation)
	}

	// The secret and name each step saw, to tell an in-place update from a
	// replacement.
	var firstSecret, firstName string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("2030-01-01T00:00:00Z", "1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(clientSecretAddr, "name",
						regexp.MustCompile(`^tenants/t-1/issuers/i-1/clients/[^/]+/client-secrets/[^/]+$`)),
					resource.TestCheckResourceAttr(clientSecretAddr, "hash_enabled", "true"),
					resource.TestCheckResourceAttr(clientSecretAddr, "expires_at", "2030-01-01T00:00:00Z"),
					func(s *terraform.State) error {
						attrs := s.RootModule().Resources[clientSecretAddr].Primary.Attributes
						firstSecret, firstName = attrs["secret"], attrs["name"]
						id := firstName[strings.LastIndex(firstName, "/")+1:]
						if !strings.HasPrefix(firstSecret, id+"_") {
							return fmt.Errorf("secret %q is not <id>_<plaintext> for id %q", firstSecret, id)
						}
						return nil
					},
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						if h.fake.mints != 1 {
							return fmt.Errorf("%d mints, want 1", h.fake.mints)
						}
						return nil
					}),
					resource.TestCheckResourceAttr("data.authwise_client_secrets.ci", "client_secrets.#", "1"),
					resource.TestCheckNoResourceAttr("data.authwise_client_secrets.ci", "client_secrets.0.secret"),
				),
			},
			{
				// expires_at patches in place: same row, same secret, no mint.
				Config: config("2031-06-30T00:00:00Z", "1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(clientSecretAddr, "expires_at", "2031-06-30T00:00:00Z"),
					resource.TestCheckResourceAttrWith(clientSecretAddr, "name", equalTo(&firstName)),
					resource.TestCheckResourceAttrWith(clientSecretAddr, "secret", equalTo(&firstSecret)),
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						if h.fake.mints != 1 {
							return fmt.Errorf("%d mints after an expires_at change, want 1", h.fake.mints)
						}
						if got := h.fake.clientSecrets[firstName].GetExpiresAt().AsTime().Format("2006-01-02"); got != "2031-06-30" {
							return fmt.Errorf("server expires_at = %s", got)
						}
						return nil
					}),
				),
			},
			{
				// A keepers change rotates: the successor is minted, then the
				// old row deleted.
				Config: config("2031-06-30T00:00:00Z", "2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(clientSecretAddr, "name", notEqualTo(&firstName)),
					resource.TestCheckResourceAttrWith(clientSecretAddr, "secret", notEqualTo(&firstSecret)),
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						if h.fake.mints != 2 {
							return fmt.Errorf("%d mints after rotation, want 2", h.fake.mints)
						}
						if _, ok := h.fake.clientSecrets[firstName]; ok {
							return fmt.Errorf("the rotated-out secret %s is still there", firstName)
						}
						if n := len(h.fake.clientSecrets); n != 1 {
							return fmt.Errorf("%d client secrets, want 1", n)
						}
						return nil
					}),
				),
			},
			{
				// The secret cannot be recovered, and keepers never reach
				// kit: an import has neither. client_id is ignored too, for a
				// reason that is not the secret's: tfinfra's import does not
				// fill scope identifiers from the name yet, which any
				// resource whose parent id is set explicitly shares.
				ResourceName:                         clientSecretAddr,
				ImportState:                          true,
				ImportStateIdFunc:                    importByName(clientSecretAddr),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"secret", "keepers.%", "keepers.rotation", "client_id"},
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if n := len(h.fake.clientSecrets); n != 0 {
				return fmt.Errorf("%d client secrets left", n)
			}
			return nil
		},
	})
}

// kit refuses an expiry that has already passed; the apply surfaces it.
func TestAccClientSecret_PastExpiryRefused(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.providerConfig() + `
resource "authwise_client" "ci" {
  display_name = "CI"
  grant_types  = ["client_credentials"]
}

resource "authwise_client_secret" "ci" {
  client_id  = authwise_client.ci.client_id
  expires_at = "2020-01-01T00:00:00Z"
}
`,
				ExpectError: regexp.MustCompile(`expires_at must be in the future`),
			},
		},
	})
}

func equalTo(want *string) resource.CheckResourceAttrWithFunc {
	return func(got string) error {
		if got != *want {
			return fmt.Errorf("got %q, want %q", got, *want)
		}
		return nil
	}
}

func notEqualTo(prior *string) resource.CheckResourceAttrWithFunc {
	return func(got string) error {
		if got == *prior {
			return fmt.Errorf("still %q; expected a new value", got)
		}
		return nil
	}
}
