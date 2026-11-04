// Profile claims in access tokens (kit#892, tf#61; apis v0.52.0): an
// audience lists the OpenID Connect scopes whose claims kit also writes
// into its access tokens.
package acctest_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// claimScopesAudience is an audience whose config is the function call's
// argument.
func claimScopesAudience(h *harness, config string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_audience" "orders" {
  display_name = "https://orders.example.com"
  config       = provider::authwise::audience_config(%s)
}
`, config)
}

// TestAccAudience_AccessTokenClaimScopes: left out, the list is absent on
// the server; set, it reaches it in order, and the plan after apply is
// empty either way.
func TestAccAudience_AccessTokenClaimScopes(t *testing.T) {

	h := newHarness(t)

	scopes := func(want ...string) resource.TestCheckFunc {
		return checkServer(func() error {
			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()
			if len(h.fake.audiences) != 1 {
				return fmt.Errorf("expected one audience on the server, got %d", len(h.fake.audiences))
			}
			for _, a := range h.fake.audiences {
				if got := a.GetConfig().GetAccessTokenClaimScopes(); !slices.Equal(got, want) {
					return fmt.Errorf("access_token_claim_scopes = %q, want %q", got, want)
				}
			}
			return nil
		})
	}

	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: claimScopesAudience(h, `{ access_token_expire_seconds = 600 }`),
				Check:  scopes(),
			},
			{
				Config:           claimScopesAudience(h, `{ access_token_expire_seconds = 600 }`),
				ConfigPlanChecks: empty,
			},
			{
				Config: claimScopesAudience(h, `{ access_token_expire_seconds = 600, access_token_claim_scopes = ["profile", "email"] }`),
				Check:  scopes("profile", "email"),
			},
			{
				Config:           claimScopesAudience(h, `{ access_token_expire_seconds = 600, access_token_claim_scopes = ["profile", "email"] }`),
				ConfigPlanChecks: empty,
			},
		},
		CheckDestroy: noneLeft(h),
	})
}

// TestAccAudience_AccessTokenClaimScopesRefused: kit refuses a scope it
// emits no claims for, and a repeat, at apply; the provider passes its
// message on.
func TestAccAudience_AccessTokenClaimScopesRefused(t *testing.T) {

	cases := map[string]struct {
		scopes string
		want   *regexp.Regexp
	}{
		"not a claim scope": {
			scopes: `["profile", "address"]`,
			want:   regexp.MustCompile(`(?s)access_token_claim_scopes:\s+"address"\s+is\s+not\s+one\s+of\s+profile,\s+email,\s+phone`),
		},
		"listed twice": {
			scopes: `["email", "email"]`,
			want:   regexp.MustCompile(`(?s)access_token_claim_scopes:\s+"email"\s+is\s+listed\s+twice`),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps: []resource.TestStep{{
					Config:      claimScopesAudience(h, fmt.Sprintf(`{ access_token_claim_scopes = %s }`, v.scopes)),
					ExpectError: v.want,
				}},
				CheckDestroy: noneLeft(h),
			})
		})
	}
}
