// Guard's settings for a kit tenant (tf#65, apis v0.57.0): no Guard
// tenant, PatchTenant an upsert, destroy writing nothing.
package acctest_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const guardSettingsAddr = "authwise_guard_tenant_settings.acme"

func TestAccGuardTenantSettings(t *testing.T) {

	h := newHarness(t)

	config := func(body string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_guard_tenant_settings" "acme" {
  tenant_id = "t-1"
%s
}
`, body)
	}

	held := func() *guardpb.Tenant {
		h.guard.mu.Lock()
		defer h.guard.mu.Unlock()
		return h.guard.tenants["tenants/t-1"]
	}

	server := func(check func(*guardpb.Tenant) error) resource.TestCheckFunc {
		return checkServer(func() error { return check(held()) })
	}

	const (
		first = `
  config = {
    deprovisioning = { enabled = true }
  }`
		second = `
  labels = { env = "prod" }
  config = {
    users          = { realms = ["r-staff"] }
    deprovisioning = { enabled = true }
  }`
	)

	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(first),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardSettingsAddr, "name", "tenants/t-1"),
					// The slug is kit's, read back.
					resource.TestCheckResourceAttr(guardSettingsAddr, "slug", "acme"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "config.deprovisioning.enabled", "true"),
					// Unset, the realms are read back, not written.
					resource.TestCheckResourceAttr(guardSettingsAddr, "config.users.realms.#", "0"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "display_name", "Acme"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "status", "ACTIVE"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "issuer", "https://id.example.com/t-1"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "audience", "a-guard"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "console_client_id", "c-console"),
					server(func(t *guardpb.Tenant) error {
						if !t.GetConfig().GetDeprovisioning().GetEnabled() {
							return fmt.Errorf("server holds deprovisioning %v; want true", t.GetConfig().GetDeprovisioning().GetEnabled())
						}
						return nil
					}),
				),
			},
			{Config: config(first), ConfigPlanChecks: empty},
			{
				Config: config(second),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(guardSettingsAddr, "labels.env", "prod"),
					resource.TestCheckResourceAttr(guardSettingsAddr, "config.users.realms.0", "r-staff"),
					server(func(t *guardpb.Tenant) error {
						if !slices.Equal(t.GetConfig().GetUsers().GetRealms(), []string{"r-staff"}) || t.GetLabels()["env"] != "prod" {
							return fmt.Errorf("server holds realms %q, labels %v", t.GetConfig().GetUsers().GetRealms(), t.GetLabels())
						}
						return nil
					}),
				),
			},
			{Config: config(second), ConfigPlanChecks: empty},
			{
				ResourceName:                         guardSettingsAddr,
				ImportState:                          true,
				ImportStateId:                        "t-1",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "tenant_id",
			},
		},
		// Destroy writes nothing: the settings stay on the tenant.
		CheckDestroy: func(*terraform.State) error {
			if t := held(); t == nil || t.GetSlug() != "acme" || !t.GetConfig().GetDeprovisioning().GetEnabled() {
				return fmt.Errorf("destroy changed Guard's settings: %v", t)
			}
			return nil
		},
	})
}

// A tenant kit has not turned Guard on for is refused at apply in
// guard-control's words.
func TestAccGuardTenantSettings_NotEnabled(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.providerConfig() + `
resource "authwise_guard_tenant_settings" "other" {
  tenant_id = "t-2"
  labels    = { env = "prod" }
}
`,
			ExpectError: regexp.MustCompile(`(?s)Guard\s+is\s+not\s+enabled\s+on\s+tenant\s+tenants/t-2`),
		}},
	})
}
