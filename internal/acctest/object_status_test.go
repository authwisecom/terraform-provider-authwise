// kit#972: an issuer, realm or audience turned off and on with `status`
// (apis v0.80.0). The change is made in place, and a kit without the
// identity.object-status feature refuses a write that sets it by name,
// rather than dropping the field.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const statusRealmAddr = "authwise_realm.r"

func (h *harness) statusRealmConfig(status string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "r" {
  display_name = "Employees"
  %s
}
`, status)
}

func TestAccObjectStatus_Realm(t *testing.T) {

	h := newHarness(t)
	var name string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Unset reads back kit's default.
				Config: h.statusRealmConfig(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(statusRealmAddr, "status", "active"),
					resource.TestCheckResourceAttrWith(statusRealmAddr, "name", func(v string) error { name = v; return nil }),
				),
			},
			{
				Config: h.statusRealmConfig(`status = "disabled"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(statusRealmAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(statusRealmAddr, "status", "disabled"),
					resource.TestCheckResourceAttrWith(statusRealmAddr, "name", equalTo(&name)),
					checkServer(func() error {
						h.fake.mu.Lock()
						defer h.fake.mu.Unlock()
						if got := h.fake.realms[name].GetStatus(); got != "disabled" {
							return fmt.Errorf("server status %q", got)
						}
						return nil
					}),
				),
			},
			{
				Config: h.statusRealmConfig(`status = "active"`),
				Check:  resource.TestCheckResourceAttr(statusRealmAddr, "status", "active"),
			},
		},
	})
}

// TestAccObjectStatus_EarlierKit: a kit without the feature still manages
// the realm; only setting status is refused.
func TestAccObjectStatus_EarlierKit(t *testing.T) {

	h := newHarness(t)
	h.fake.capabilities = func() (*identitypb.InstallCapabilities, error) {
		return &identitypb.InstallCapabilities{
			Features:   []string{"guard.tenant-collapse", "identity.vocabulary"},
			KitVersion: "1.87.2",
		}, nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: h.statusRealmConfig("")},
			{
				Config:      h.statusRealmConfig(`status = "disabled"`),
				ExpectError: regexp.MustCompile(`(?s)identity\.object-status.*kit 1\.87\.2`),
			},
		},
	})
}
