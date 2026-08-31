package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const accessPrefix = "tenants/t-1/issuers/i-1/audiences/a-1"

// TestAccAccessCatalog_Lifecycle exercises the access surface the guard
// prerequisite needs: caller-assigned ids compose the resource names, the
// role's permission set is authoritative, and the binding lands with the role
// it names.
func TestAccAccessCatalog_Lifecycle(t *testing.T) {

	h := newHarness(t)

	catalog := func(members, description string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_access_permission" "get" {
  access_permission_id = "guardcontrol.tenants.get"
  service              = "guardcontrol"
  kind                 = "custom"
}

resource "authwise_access_permission" "update" {
  access_permission_id = "guardcontrol.tenants.update"
  service              = "guardcontrol"
  kind                 = "custom"
}

resource "authwise_access_role" "admin" {
  access_role_id = "guardcontrol.admin"
  kind           = "custom"
  description    = %q
}

resource "authwise_access_role_access_permissions" "admin" {
  access_role        = authwise_access_role.admin.name
  access_permissions = [%s]
}

resource "authwise_access_binding" "admin" {
  subject_type = "user"
  subject_id   = "u-01"
  role_name    = authwise_access_role.admin.access_role_id
}
`, description, members)
	}

	const (
		role   = accessPrefix + "/access-roles/guardcontrol.admin"
		get    = "authwise_access_permission.get.name"
		update = "authwise_access_permission.update.name"
	)

	wantMembers := func(want ...string) resource.TestCheckFunc {
		return checkServer(func() error {
			got := h.access.rolePermissions(role)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				return fmt.Errorf("role permissions = %v, want %v", got, want)
			}
			return nil
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: catalog(get, "Full guard-control console administration."),
				Check: resource.ComposeAggregateTestCheckFunc(
					// The caller's id is the last segment of the name the
					// server composes — the caller-named lane end to end.
					resource.TestCheckResourceAttr("authwise_access_permission.get", "name",
						accessPrefix+"/access-permissions/guardcontrol.tenants.get"),
					resource.TestCheckResourceAttr("authwise_access_permission.get", "access_permission_id",
						"guardcontrol.tenants.get"),
					resource.TestCheckResourceAttr("authwise_access_role.admin", "name", role),
					resource.TestCheckResourceAttr("authwise_access_role.admin", "access_role_id", "guardcontrol.admin"),
					wantMembers(accessPrefix+"/access-permissions/guardcontrol.tenants.get"),
					resource.TestMatchResourceAttr("authwise_access_binding.admin", "name",
						regexp.MustCompile(`^`+regexp.QuoteMeta(accessPrefix)+`/access-bindings/axb-\d+$`)),
					checkServer(func() error {
						b := h.access.onlyBinding()
						switch {
						case b == nil:
							return fmt.Errorf("no binding on the server")
						case b.GetRoleName() != "guardcontrol.admin":
							return fmt.Errorf("binding role_name = %q", b.GetRoleName())
						case b.GetSubjectId() != "u-01":
							return fmt.Errorf("binding subject_id = %q", b.GetSubjectId())
						}
						return nil
					}),
				),
			},
			{
				// Growing the catalog extends the authoritative set, and the
				// role's own patch carries only the changed field.
				Config: catalog(get+", "+update, "Console administration."),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_access_role.admin", "description", "Console administration."),
					resource.TestCheckResourceAttr("authwise_access_role_access_permissions.admin", "access_permissions.#", "2"),
					wantMembers(
						accessPrefix+"/access-permissions/guardcontrol.tenants.get",
						accessPrefix+"/access-permissions/guardcontrol.tenants.update",
					),
				),
			},
			{
				// A permission associated out of band is removed on the next
				// apply: the set is authoritative.
				PreConfig: func() {
					h.access.mu.Lock()
					h.access.rolePerms[role][accessPrefix+"/access-permissions/rogue"] = true
					h.access.mu.Unlock()
				},
				Config: catalog(get+", "+update, "Console administration."),
				Check: wantMembers(
					accessPrefix+"/access-permissions/guardcontrol.tenants.get",
					accessPrefix+"/access-permissions/guardcontrol.tenants.update",
				),
			},
			{
				// Import must fill access_permission_id from the name, or the
				// next plan would force replacement.
				ResourceName: "authwise_access_permission.get",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_access_permission.get"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			h.access.mu.Lock()
			defer h.access.mu.Unlock()
			switch {
			case len(h.access.permissions) != 0:
				return fmt.Errorf("%d access permissions left after destroy", len(h.access.permissions))
			case len(h.access.roles) != 0:
				return fmt.Errorf("%d access roles left after destroy", len(h.access.roles))
			case len(h.access.bindings) != 0:
				return fmt.Errorf("%d access bindings left after destroy", len(h.access.bindings))
			}
			return nil
		},
	})
}

// TestAccAccessBinding_UnknownRole covers the kit#296 hazard: the API stores
// a binding naming a role that does not exist in its audience, the binding
// then grants nothing, and every later plan is clean — so the provider
// refuses to write one.
func TestAccAccessBinding_UnknownRole(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_access_binding" "typo" {
  subject_type = "user"
  subject_id   = "u-01"
  role_name    = "guardcontrol.adnim"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`access role not found in this audience`),
			},
		},
	})

	h.access.mu.Lock()
	defer h.access.mu.Unlock()
	if n := len(h.access.bindings); n != 0 {
		t.Fatalf("the rejected binding reached the server: %d stored", n)
	}
}

// TestAccAccessPermission_IDCollision proves a duplicate caller-assigned id
// surfaces as the API's rejection rather than silently adopting the row.
func TestAccAccessPermission_IDCollision(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_access_permission" "first" {
  access_permission_id = "guardcontrol.tenants.get"
  service              = "guardcontrol"
}

resource "authwise_access_permission" "second" {
  access_permission_id = "guardcontrol.tenants.get"
  service              = "guardcontrol"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`already exists`),
			},
		},
	})
}
