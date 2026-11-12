package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/protobuf/proto"
)

const brandingAddr = "authwise_tenant_branding.this"

// TestAccTenantBranding_Lifecycle (tf#75, kit#953): the brand set, changed,
// edited behind Terraform's back, imported and destroyed, beside an
// authwise_tenant_config that leaves branding out, so the two never fight.
// Destroying the whole configuration works because the brand is cleared
// before its theme and logo go: the fake, as kit, refuses deleting either
// while the brand names it.
func TestAccTenantBranding_Lifecycle(t *testing.T) {

	h := newHarness(t)

	config := func(brand string) string {
		return h.providerConfig() + brandAssets + fmt.Sprintf(`
resource "authwise_theme" "look" {
  display_name = "Northwind"
}

resource "authwise_tenant_branding" "this" {
  %s
}

data "authwise_tenant_config" "this" {
  access_token_expire_seconds = 600
}

resource "authwise_tenant_config" "this" {
  config = data.authwise_tenant_config.this.json
}
`, brand)
	}
	full := `display_name  = "Northwind Supply"
  theme_id      = authwise_theme.look.theme_id
  logo_asset_id = authwise_asset_content.logo.asset_id`

	attr := func(s *terraform.State, addr, a string) string {
		return s.RootModule().Resources[addr].Primary.Attributes[a]
	}
	// held asserts the stored brand, and that tenant_config's write left
	// it alone.
	held := func(want func(*terraform.State) *corepb.Branding) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			tn := h.tenancy.tenant("tenants/t-1")
			got, w := tn.GetConfig().GetBranding(), want(s)
			if got == nil {
				got = &corepb.Branding{}
			}
			if !proto.Equal(got, w) {
				return fmt.Errorf("branding = %v, want %v", got, w)
			}
			if tn.GetConfig().GetAccessTokenExpireSeconds() != 600 {
				return fmt.Errorf("tenant_config not written: %v", tn.GetConfig())
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(full),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(brandingAddr, "name", "tenants/t-1/branding"),
					resource.TestCheckNoResourceAttr(brandingAddr, "logo_dark_asset_id"),
					held(func(s *terraform.State) *corepb.Branding {
						return &corepb.Branding{
							DisplayName: "Northwind Supply",
							ThemeId:     attr(s, "authwise_theme.look", "theme_id"),
							LogoAssetId: attr(s, "authwise_asset_content.logo", "asset_id"),
						}
					}),
				),
			},
			{Config: config(full), PlanOnly: true},
			{
				// The theme dropped from the brand: cleared, as the
				// brand is authoritative.
				Config: config(`display_name  = "Northwind"
  logo_asset_id = authwise_asset_content.logo.asset_id`),
				Check: held(func(s *terraform.State) *corepb.Branding {
					return &corepb.Branding{DisplayName: "Northwind", LogoAssetId: attr(s, "authwise_asset_content.logo", "asset_id")}
				}),
			},
			{
				// Renamed in the console: drift, put back.
				PreConfig: func() {
					h.tenancy.mu.Lock()
					h.tenancy.tenants["tenants/t-1"].GetConfig().GetBranding().DisplayName = "Edited"
					h.tenancy.mu.Unlock()
				},
				Config: config(`display_name  = "Northwind"
  logo_asset_id = authwise_asset_content.logo.asset_id`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(brandingAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr(brandingAddr, "display_name", "Northwind"),
			},
			{
				ResourceName:    brandingAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   "t-1",
				Config: config(`display_name  = "Northwind"
  logo_asset_id = authwise_asset_content.logo.asset_id`),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if b := h.tenancy.tenant("tenants/t-1").GetConfig().GetBranding(); b.GetDisplayName() != "" || b.GetLogoAssetId() != "" || b.GetThemeId() != "" {
				return fmt.Errorf("brand not cleared: %v", b)
			}
			return nil
		},
	})
}

// TestAccTenantBranding_Refusals: kit's checks reach the plan's author.
func TestAccTenantBranding_Refusals(t *testing.T) {

	h := newHarness(t)
	cfg := func(body string) string {
		return h.providerConfig() + "resource \"authwise_tenant_branding\" \"this\" {\n" + body + "\n}\n"
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: cfg(`display_name = ""`), PlanOnly: true, ExpectError: regexp.MustCompile(`at least 1`)},
			{Config: cfg(`theme_id = "th-none"`), ExpectError: regexp.MustCompile(`(?s)kit refused the brand.*no theme th-none`)},
			{Config: cfg(`display_name = "<b>"`), ExpectError: regexp.MustCompile(`(?s)kit refused the brand.*none of`)},
		},
	})
}

// TestAccIssuer_BrandedTenantStartingProfile (kit#953, apis v0.67.0): on a
// tenant whose brand names a theme, kit names the appearance profile it
// gives a new issuer in the issuer's config, output only. An issuer with a
// configured config is created without an inconsistent result, plans no
// change after, and never shows the id; a config change keeps kit's id
// where it is.
func TestAccIssuer_BrandedTenantStartingProfile(t *testing.T) {

	h := newHarness(t)
	h.fake.brandedTenant = true

	config := func(seconds int) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config    = jsonencode({ accessTokenExpireSeconds = %d })
}
`, seconds)
	}
	held := func(*terraform.State) error {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		for _, i := range h.fake.issuers {
			if i.GetConfig().GetStartingAppearanceProfileId() == "" {
				return fmt.Errorf("kit's starting profile id was lost: %v", i.GetConfig())
			}
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_issuer.login", "config", `{"accessTokenExpireSeconds":600}`),
					held,
				),
			},
			{Config: config(600), PlanOnly: true},
			{
				Config: config(900),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_issuer.login", "config", `{"accessTokenExpireSeconds":900}`),
					held,
				),
			},
			{Config: config(900), PlanOnly: true},
		},
	})
}
