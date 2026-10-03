package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccUser_ContactIsOutputOnly: a user's email and phone are kit's to
// derive (kit#662, kit#666). They read back, and an update of a user who has
// an address succeeds, because the provider never echoes the address into
// the write; kit refuses it whatever the value.
func TestAccUser_ContactIsOutputOnly(t *testing.T) {

	h := newHarness(t)

	config := func(given string) string {
		return h.providerConfig() + realmForFactors + fmt.Sprintf(`
resource "authwise_user" "u" {
  realm_id   = local.realm_id
  given_name = %q
}
`, given)
	}

	// The address arrives the way an accepted invitation brings it: outside
	// the user resource.
	invite := func(_ *terraform.State) error {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		for name := range h.fake.users {
			h.fake.contact[name] = "ada@example.com"
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("Ada"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("authwise_user.u", "email"),
					invite,
				),
			},
			{
				Config: config("Augusta"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_user.u", "given_name", "Augusta"),
					resource.TestCheckResourceAttr("authwise_user.u", "email", "ada@example.com"),
					resource.TestCheckResourceAttr("authwise_user.u", "email_verified", "true"),
				),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if n := len(h.fake.users) + len(h.fake.realms); n != 0 {
				return fmt.Errorf("%d objects left on the server after destroy", n)
			}
			return nil
		},
	})
}

// TestAccUser_ContactNotSettable: a configuration that sets an address fails
// at plan, naming the attribute, rather than at apply.
func TestAccUser_ContactNotSettable(t *testing.T) {

	h := newHarness(t)

	for _, attr := range []string{`email = "ada@example.com"`, `email_verified = true`, `phone_number = "+14155550100"`, `phone_number_verified = true`} {
		t.Run(attr, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config: h.providerConfig() + fmt.Sprintf(`
resource "authwise_user" "u" {
  realm_id = "r-1"
  %s
}
`, attr),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(`Invalid Configuration for Read-Only Attribute`),
				}},
			})
		})
	}
}
