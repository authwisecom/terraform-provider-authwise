// Service accounts (kit#756, #37): a client of the admin issuer its tenant
// owns, made through the public tenancy service, and its secrets, minted
// once and kept in state. The steps walk a mint, a refresh, a
// keepers-driven rotation with its grace, a secret that lapsed, import,
// and kit's refusal to delete an account that still holds a role.
package acctest_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	serviceAccountAddr       = "authwise_service_account.ci"
	serviceAccountSecretAddr = "authwise_service_account_secret.ci"
)

func (h *harness) serviceAccountConfig(rotation, extra string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_service_account" "ci" {
  display_name = "terraform"
}

resource "authwise_service_account_secret" "ci" {
  service_account = authwise_service_account.ci.name
  expires_at      = "2030-01-01T00:00:00Z"
  grace           = "72h"
  keepers         = { rotation = %q }
}
%s`, rotation, extra)
}

func TestAccServiceAccount_MintRotate(t *testing.T) {

	h := newHarness(t)

	var firstSecret, firstID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.serviceAccountConfig("1", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(serviceAccountAddr, "name", regexp.MustCompile(`^tenants/t-1/service-accounts/c-[0-9]+$`)),
					resource.TestCheckResourceAttr(serviceAccountAddr, "tenant_id", "t-1"),
					resource.TestMatchResourceAttr(serviceAccountAddr, "client_id", regexp.MustCompile(`^c-[0-9]+$`)),
					resource.TestCheckResourceAttr(serviceAccountAddr, "status", "CLIENT_STATUS_ACTIVE"),
					resource.TestCheckNoResourceAttr(serviceAccountAddr, "expires_at"),
					resource.TestCheckResourceAttr(serviceAccountSecretAddr, "token_url", fakeAdminTokenURL),
					resource.TestCheckResourceAttr(serviceAccountSecretAddr, "audience", "a-01"),
					resource.TestCheckResourceAttr(serviceAccountSecretAddr, "rotate", "true"),
					resource.TestCheckResourceAttrSet(serviceAccountSecretAddr, "created_at"),
					func(s *terraform.State) error {
						attrs := s.RootModule().Resources[serviceAccountSecretAddr].Primary.Attributes
						firstSecret, firstID = attrs["secret"], attrs["secret_id"]
						if !strings.HasPrefix(firstSecret, firstID+"_") {
							return fmt.Errorf("secret %q is not <secret_id>_<plaintext> for %q", firstSecret, firstID)
						}
						return nil
					},
				),
			},
			{
				// A refresh keeps the secret, and the account now lists it.
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(serviceAccountSecretAddr, "secret", equalTo(&firstSecret)),
					resource.TestCheckResourceAttr(serviceAccountAddr, "secrets.#", "1"),
					resource.TestCheckResourceAttrWith(serviceAccountAddr, "secrets.0.id", equalTo(&firstID)),
					resource.TestCheckResourceAttr(serviceAccountAddr, "secrets.0.expires_at", "2030-01-01T00:00:00Z"),
				),
			},
			{
				// A keepers change mints a successor; the rotation gives the
				// old secret the 72h grace rather than revoking it.
				Config: h.serviceAccountConfig("2", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(serviceAccountSecretAddr, plancheck.ResourceActionDestroyBeforeCreate),
						plancheck.ExpectResourceAction(serviceAccountAddr, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(serviceAccountSecretAddr, "secret", notEqualTo(&firstSecret)),
					checkServer(func() error {
						old := h.tenancy.secret(firstID)
						if old == nil {
							return fmt.Errorf("the rotated secret %s is gone; want it kept for the grace", firstID)
						}
						left := time.Until(old.GetExpiresAt().AsTime())
						if left < 71*time.Hour || left > 72*time.Hour {
							return fmt.Errorf("the rotated secret lapses in %s, want 72h", left)
						}
						return nil
					}),
				),
			},
			{
				// A secret that lapsed is gone from state: the next apply
				// mints another.
				PreConfig: func() {
					h.tenancy.mu.Lock()
					var id string
					for _, sa := range h.tenancy.accounts {
						id = sa.GetSecrets()[len(sa.GetSecrets())-1].GetId()
					}
					h.tenancy.mu.Unlock()
					h.tenancy.expire(id)
				},
				Config: h.serviceAccountConfig("2", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(serviceAccountSecretAddr, plancheck.ResourceActionCreate),
					},
				},
				Check: checkServer(func() error {
					h.tenancy.mu.Lock()
					defer h.tenancy.mu.Unlock()
					if h.tenancy.mints != 3 {
						return fmt.Errorf("%d mints, want 3", h.tenancy.mints)
					}
					return nil
				}),
			},
			{
				// The account's secrets catch up on the next refresh: the
				// live one and the rotated one in its grace.
				RefreshState: true,
				Check:        resource.TestCheckResourceAttr(serviceAccountAddr, "secrets.#", "2"),
			},
			{
				ResourceName:                         serviceAccountAddr,
				ImportState:                          true,
				ImportStateIdFunc:                    importByName(serviceAccountAddr),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
		CheckDestroy: func(*terraform.State) error {
			h.tenancy.mu.Lock()
			defer h.tenancy.mu.Unlock()
			if n := len(h.tenancy.accounts); n != 0 {
				return fmt.Errorf("%d service accounts remain after destroy", n)
			}
			return nil
		},
	})
}

// A binding that names the account by expression is destroyed before it;
// one made outside terraform keeps kit from deleting the account, and the
// refusal says what to do.
func TestAccServiceAccount_BoundRefusesDelete(t *testing.T) {

	h := newHarness(t)

	binding := `
resource "authwise_access_role" "admin" {
  access_role_id = "tenant.admin"
}

resource "authwise_access_binding" "ci" {
  subject_type = "client"
  subject_id   = authwise_service_account.ci.client_id
  role_name    = authwise_access_role.admin.access_role_id
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.serviceAccountConfig("1", binding),
				Check:  resource.TestCheckResourceAttrPair("authwise_access_binding.ci", "subject_id", serviceAccountAddr, "client_id"),
			},
			{
				// Removing both: terraform orders the binding first.
				Config: h.providerConfig(),
			},
			{
				Config: h.serviceAccountConfig("1", ""),
			},
			{
				PreConfig: func() {
					h.tenancy.mu.Lock()
					var client string
					for _, sa := range h.tenancy.accounts {
						client = sa.GetClientId()
					}
					h.tenancy.mu.Unlock()
					h.access.mu.Lock()
					defer h.access.mu.Unlock()
					name := accessPrefix + "/access-bindings/axb-outside"
					h.access.bindings[name] = &corepb.AccessBinding{Name: name, SubjectType: "client", SubjectId: client, RoleName: "tenant.admin"}
				},
				Config:      h.providerConfig(),
				ExpectError: regexp.MustCompile(`(?s)the\s+service\s+account\s+is\s+still\s+bound.*still\s+bound:.*axb-outside`),
			},
			{
				PreConfig: func() {
					h.access.mu.Lock()
					defer h.access.mu.Unlock()
					clear(h.access.bindings)
				},
				Config: h.providerConfig(),
			},
		},
	})
}

// roles bind at create, change in place, refuse a role kit does not allow,
// and go with the account on destroy.
func TestAccServiceAccount_Roles(t *testing.T) {

	h := newHarness(t)

	config := func(roles string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_service_account" "ci" {
  display_name = "terraform"
  roles        = %s
}
`, roles)
	}

	var name string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(`["tenant.admin"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(serviceAccountAddr, "roles.#", "1"),
					resource.TestCheckTypeSetElemAttr(serviceAccountAddr, "roles.*", "tenant.admin"),
					resource.TestCheckResourceAttrWith(serviceAccountAddr, "name", func(v string) error {
						name = v
						return nil
					}),
				),
			},
			{
				Config: config(`["tenant.viewer", "guardcontrol.admin"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(serviceAccountAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(serviceAccountAddr, "roles.#", "2"),
					resource.TestCheckTypeSetElemAttr(serviceAccountAddr, "roles.*", "guardcontrol.admin"),
					resource.TestCheckResourceAttrWith(serviceAccountAddr, "name", equalTo(&name)),
					checkServer(func() error {
						h.tenancy.mu.Lock()
						defer h.tenancy.mu.Unlock()
						if got := h.tenancy.accounts[name].GetRoles(); !slices.Equal(got, []string{"guardcontrol.admin", "tenant.viewer"}) {
							return fmt.Errorf("server roles = %v", got)
						}
						return nil
					}),
				),
			},
			{
				Config:      config(`["operator"]`),
				ExpectError: regexp.MustCompile(`(?s)kit\s+refused\s+the\s+service\s+account's\s+roles.*"operator"\s+is\s+not\s+one`),
			},
			{
				// Unset, roles are left alone and read back: no diff.
				PreConfig: func() {
					h.tenancy.mu.Lock()
					defer h.tenancy.mu.Unlock()
					h.tenancy.accounts[name].Roles = []string{"tenant.admin"}
				},
				Config: h.providerConfig() + `
resource "authwise_service_account" "ci" {
  display_name = "terraform"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckTypeSetElemAttr(serviceAccountAddr, "roles.*", "tenant.admin"),
			},
			{
				Config: config(`[]`),
				Check:  resource.TestCheckResourceAttr(serviceAccountAddr, "roles.#", "0"),
			},
			{
				// Destroy unbinds the roles, then deletes.
				Config: config(`["tenant.admin"]`),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			h.tenancy.mu.Lock()
			defer h.tenancy.mu.Unlock()
			if n := len(h.tenancy.accounts); n != 0 {
				return fmt.Errorf("%d service accounts remain after destroy", n)
			}
			return nil
		},
	})
}
