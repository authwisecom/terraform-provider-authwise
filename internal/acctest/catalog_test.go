// Domain, Scope and AppearanceProfile (#15): the keying and collections the
// provider took from kit's http annotations — caller-named domain and scope
// ids, and appearance profiles under the issuer in "appearance-profiles" —
// exercised end to end, import included.
package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func importByName(addr string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		return s.RootModule().Resources[addr].Primary.Attributes["name"], nil
	}
}

func TestAccDomain_CallerNamed(t *testing.T) {

	h := newHarness(t)

	domain := func(team string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "login" {
  domain_id = "login.example.com"
  labels    = { team = %q }
}
`, team)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: domain("identity"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_domain.login", "name", "tenants/t-1/domains/login.example.com"),
					resource.TestCheckResourceAttr("authwise_domain.login", "domain_id", "login.example.com"),
				),
			},
			{
				Config: domain("platform"),
				Check: checkServer(func() error {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					if got := h.fake.domains["tenants/t-1/domains/login.example.com"].GetLabels()["team"]; got != "platform" {
						return fmt.Errorf("labels.team = %q", got)
					}
					return nil
				}),
			},
			{
				// Import fills domain_id from the name's last segment; without
				// it the required attribute would force a replace.
				ResourceName:                         "authwise_domain.login",
				ImportState:                          true,
				ImportStateIdFunc:                    importByName("authwise_domain.login"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if n := len(h.fake.domains); n != 0 {
				return fmt.Errorf("%d domains left", n)
			}
			return nil
		},
	})
}

func TestAccScope_CallerNamedWithAccessPermissions(t *testing.T) {

	h := newHarness(t)

	scope := func(members string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_access_permission" "users_read" {
  access_permission_id = "api.users.read"
  service              = "api"
}

resource "authwise_access_permission" "users_list" {
  access_permission_id = "api.users.list"
  service              = "api"
}

resource "authwise_scope" "read" {
  scope_id = "users.read"
}

resource "authwise_scope_access_permissions" "read" {
  scope              = authwise_scope.read.name
  access_permissions = [%s]
}
`, members)
	}

	const scopeName = accessPrefix + "/scopes/users.read"
	members := func(want ...string) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			got := h.fake.scopePermissionsLocked(scopeName)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				return fmt.Errorf("scope permissions = %v, want %v", got, want)
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: scope(`authwise_access_permission.users_read.name`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_scope.read", "name", scopeName),
					members(accessPrefix+"/access-permissions/api.users.read"),
				),
			},
			{
				// Authoritative: the set becomes exactly what is declared.
				Config: scope(`authwise_access_permission.users_list.name`),
				Check:  members(accessPrefix + "/access-permissions/api.users.list"),
			},
			{
				ResourceName:                         "authwise_scope.read",
				ImportState:                          true,
				ImportStateIdFunc:                    importByName("authwise_scope.read"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

func TestAccAppearanceProfile_IssuerScoped(t *testing.T) {

	h := newHarness(t)

	profile := func(displayName string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_appearance_profile" "brand" {
  display_name = %q
  content      = jsonencode({ headline = "Welcome back" })
}
`, displayName)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: profile("Brand"),
				Check: resource.TestMatchResourceAttr("authwise_appearance_profile.brand", "name",
					regexp.MustCompile(`^tenants/t-1/issuers/i-1/appearance-profiles/ap-\d+$`)),
			},
			{
				Config: profile("Brand 2"),
				Check:  resource.TestCheckResourceAttr("authwise_appearance_profile.brand", "display_name", "Brand 2"),
			},
			{
				ResourceName:                         "authwise_appearance_profile.brand",
				ImportState:                          true,
				ImportStateIdFunc:                    importByName("authwise_appearance_profile.brand"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				// content is JSON: an import reads protojson's formatting.
				ImportStateVerifyIgnore: []string{"content"},
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if n := len(h.fake.profiles); n != 0 {
				return fmt.Errorf("%d appearance profiles left", n)
			}
			return nil
		},
	})
}
