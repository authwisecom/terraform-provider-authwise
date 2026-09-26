// Package acctest_test is the provider's acceptance tier: real terraform
// CLI runs (terraform-plugin-testing) against the real provider binary
// surface. Tier 1 (this file, default) targets an in-memory AIP server plus
// a fake OAuth endpoint — the whole stack short of kit itself, no external
// infrastructure. Tier 2 points the same lifecycles at a real kit stack and
// is env-gated (AUTHWISE_ACC_*); see the task-#10 follow-up.
//
// Run with: TF_ACC=1 go test ./internal/acctest/... (make testacc)
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// checkServer wraps a fake-server assertion as a state check.
func checkServer(f func() error) resource.TestCheckFunc {
	return func(_ *terraform.State) error { return f() }
}

// onlyRealm returns the single realm on the fake server.
func onlyRealm(t *testing.T, m map[string]*corepb.Realm) *corepb.Realm {
	t.Helper()
	if len(m) != 1 {
		t.Fatalf("expected exactly one realm on the server, got %d", len(m))
	}
	for _, r := range m {
		return r
	}
	return nil
}

func protoFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"authwise": providerserver.NewProtocol6WithError(provider.New("test")()),
	}
}

func TestAccRealm_Lifecycle(t *testing.T) {

	h := newHarness(t)

	realm := func(displayName string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_realm" "test" {
  display_name = %q
  labels = {
    team = "platform"
  }
}
`, displayName)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: realm("Employees"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("authwise_realm.test", "name",
						regexp.MustCompile(`^tenants/t-1/realms/r-\d+$`)),
					resource.TestCheckResourceAttr("authwise_realm.test", "display_name", "Employees"),
					resource.TestCheckResourceAttr("authwise_realm.test", "labels.team", "platform"),
				),
			},
			{
				// Update patches exactly the changed field.
				Config: realm("Staff"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_realm.test", "display_name", "Staff"),
					checkServer(func() error {
						r := onlyRealm(t, h.fake.realms)
						if r.GetDisplayName() != "Staff" {
							return fmt.Errorf("server display_name = %q", r.GetDisplayName())
						}
						// labels is the witness that the patch was surgical:
						// it is set in the fixture, semantically unrelated to
						// display_name, and carried by the retired
						// user_database_type before kit#334 removed it.
						if r.GetLabels()["team"] != "platform" {
							return fmt.Errorf("untouched field changed: labels = %v", r.GetLabels())
						}
						return nil
					}),
				),
			},
			{
				ResourceName: "authwise_realm.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["authwise_realm.test"].Primary.Attributes["name"], nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if n := len(h.fake.realms); n != 0 {
				return fmt.Errorf("%d realms left on the server after destroy", n)
			}
			return nil
		},
	})

	if h.fake.lastAuthorization != "Bearer acc-test-token" {
		t.Fatalf("bearer flow broken: authorization = %q", h.fake.lastAuthorization)
	}
	if h.tokenCalls < 1 {
		t.Fatal("token endpoint was never called")
	}
}

func TestAccAccessRole_ScopeOverride(t *testing.T) {

	h := newHarness(t)

	// audience_id overridden per-resource; tenant/issuer from provider
	// defaults — proves three-level parent composition.
	config := h.providerConfig() + `
resource "authwise_access_role" "test" {
  audience_id    = "a-override"
  access_role_id = "admin"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_access_role.test", "name",
						"tenants/t-1/issuers/i-1/audiences/a-override/access-roles/admin"),
					resource.TestCheckResourceAttr("authwise_access_role.test", "audience_id", "a-override"),
				),
			},
		},
	})
}

func TestAccProviderGoogleConfig_DataSource(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
data "authwise_provider_google" "sso" {
  client_id = "google-client"
  client_secret_ref = {
    name = "tenants/t-1/secrets/google"
  }
  scopes = ["openid", "email"]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("data.authwise_provider_google.sso", "any",
						regexp.MustCompile(`"@type":\s*"type.googleapis.com/authwise.types.core.v1alpha1.ProviderGoogle"`)),
					resource.TestMatchResourceAttr("data.authwise_provider_google.sso", "any",
						regexp.MustCompile(`"clientId":\s*"google-client"`)),
					// The secret travels by reference; the config has no field
					// that could hold the material.
					resource.TestMatchResourceAttr("data.authwise_provider_google.sso", "any",
						regexp.MustCompile(`"clientSecretRef":\s*\{\s*"name":\s*"tenants/t-1/secrets/google"`)),
				),
			},
		},
	})
}

func TestAccRealm_OutOfBandDelete(t *testing.T) {

	h := newHarness(t)

	config := h.providerConfig() + `
resource "authwise_realm" "test" {
  display_name = "Doomed"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				// Delete server-side; the refresh must plan a re-create
				// instead of erroring.
				PreConfig: func() {
					h.fake.mu.Lock()
					defer h.fake.mu.Unlock()
					for k := range h.fake.realms {
						delete(h.fake.realms, k)
					}
				},
				Config:             config,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}
