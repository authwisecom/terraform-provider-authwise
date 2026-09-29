// Plural data sources (tfinfra v0.0.15): every entity under a parent, with
// the stub paging two at a time so each read has to follow tokens. count
// rather than for_each: terraform-plugin-testing's state shim cannot hold
// string instance keys (see example_test.go).
package acctest_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccRealms_ListsTheTenant: tenant scope from the provider default,
// five realms across three pages.
func TestAccRealms_ListsTheTenant(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_realm" "r" {
  count        = 5
  display_name = ["alpha", "bravo", "charlie", "delta", "echo"][count.index]
}

data "authwise_realms" "all" {
  depends_on = [authwise_realm.r]
}

output "realm_names" {
  value = join(",", sort([for r in data.authwise_realms.all.realms : r.display_name]))
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.authwise_realms.all", "realms.#", "5"),
					resource.TestCheckOutput("realm_names", "alpha,bravo,charlie,delta,echo"),
				),
			},
		},
	})
}

// TestAccProviders_ListsARealm: realm scope set on the data source, and a
// provider in another realm stays out.
func TestAccProviders_ListsARealm(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_realm" "a" {
  display_name = "A"
}

resource "authwise_realm" "b" {
  display_name = "B"
}

resource "authwise_provider" "a" {
  count         = 3
  realm_id      = authwise_realm.a.realm_id
  display_name  = ["one", "two", "three"][count.index]
  provider_type = "usernamePassword"
}

resource "authwise_provider" "b" {
  realm_id      = authwise_realm.b.realm_id
  display_name  = "elsewhere"
  provider_type = "usernamePassword"
}

data "authwise_providers" "a" {
  realm_id   = authwise_realm.a.realm_id
  depends_on = [authwise_provider.a, authwise_provider.b]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.authwise_providers.a", "providers.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs("data.authwise_providers.a", "providers.*", map[string]string{
						"display_name":  "two",
						"provider_type": "usernamePassword",
					}),
				),
			},
		},
	})
}

// TestAccAccessPermissions_ListFillsTheID: audience scope, and each item's
// caller-assigned id filled from its name.
func TestAccAccessPermissions_ListFillsTheID(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_access_permission" "p" {
  count                = 3
  access_permission_id = ["api.users.read", "api.users.list", "api.users.write"][count.index]
  service              = "api"
}

data "authwise_access_permissions" "all" {
  depends_on = [authwise_access_permission.p]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.authwise_access_permissions.all", "access_permissions.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs("data.authwise_access_permissions.all", "access_permissions.*", map[string]string{
						"access_permission_id": "api.users.list",
						"name":                 accessPrefix + "/access-permissions/api.users.list",
						"service":              "api",
					}),
				),
			},
		},
	})
}
