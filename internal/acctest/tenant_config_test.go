package acctest_test

import (
	"encoding/base64"
	"fmt"
	"maps"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/protobuf/proto"
)

const tenantConfigAddr = "authwise_tenant_config.this"

// brandAssets are two logos with content, as a tenant's brand is set up:
// the config names each through its content, so Terraform destroys the
// config before the content and the asset.
var brandAssets = fmt.Sprintf(`
resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.png"
  mime_type    = "image/png"
}

resource "authwise_asset_content" "logo" {
  asset_id       = authwise_asset.logo.asset_id
  content_base64 = %[1]q
}

resource "authwise_asset" "logo_dark" {
  display_name = "Logo (dark)"
  path         = "/logo-dark.png"
  mime_type    = "image/png"
}

resource "authwise_asset_content" "logo_dark" {
  asset_id       = authwise_asset.logo_dark.asset_id
  content_base64 = %[1]q
}
`, base64.StdEncoding.EncodeToString([]byte("\x89PNG")))

// TestAccTenantConfig_BrandLifecycle (#40): a tenant's brand set, changed,
// edited behind Terraform's back, imported and destroyed. The tenant's
// display_name, classification and owner are not this resource's, and
// every write leaves them as awtenant made them. Destroying the whole
// configuration works only because the config is cleared before its
// assets go: the fake, as kit, refuses removing a logo the brand names.
func TestAccTenantConfig_BrandLifecycle(t *testing.T) {

	h := newHarness(t)

	config := func(brand, extra, labels string) string {
		return h.providerConfig() + brandAssets + fmt.Sprintf(`
data "authwise_tenant_config" "this" {
  access_token_expire_seconds = 600
  branding = {
    %s
  }
  %s
}

resource "authwise_tenant_config" "this" {
  config = data.authwise_tenant_config.this.json
  %s
}
`, brand, extra, labels)
	}
	light := `logo_asset_id = authwise_asset_content.logo.asset_id`
	both := light + "\n    logo_dark_asset_id = authwise_asset_content.logo_dark.asset_id"

	assetID := func(s *terraform.State, addr string) string {
		return s.RootModule().Resources[addr].Primary.Attributes["asset_id"]
	}

	// held asserts the stored tenant: the brand and labels this resource
	// wrote, and the fields it does not manage as seeded.
	held := func(labels map[string]string, dark bool) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			tn := h.tenancy.tenant("tenants/t-1")
			seed := seedTenant()
			if tn.GetDisplayName() != seed.GetDisplayName() || tn.GetOwner() != seed.GetOwner() || tn.GetClassification() != seed.GetClassification() {
				return fmt.Errorf("unmanaged fields changed: %v", tn)
			}
			want := &corepb.Branding{LogoAssetId: assetID(s, "authwise_asset_content.logo")}
			if dark {
				want.LogoDarkAssetId = assetID(s, "authwise_asset_content.logo_dark")
			}
			if !proto.Equal(tn.GetConfig().GetBranding(), want) {
				return fmt.Errorf("branding = %v, want %v", tn.GetConfig().GetBranding(), want)
			}
			if tn.GetConfig().GetAccessTokenExpireSeconds() != 600 {
				return fmt.Errorf("access_token_expire_seconds = %d", tn.GetConfig().GetAccessTokenExpireSeconds())
			}
			if !maps.Equal(tn.GetLabels(), labels) {
				return fmt.Errorf("labels = %v, want %v", tn.GetLabels(), labels)
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(light, "", `labels = { brand = "v1" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tenantConfigAddr, "name", "tenants/t-1"),
					resource.TestCheckResourceAttr(tenantConfigAddr, "tenant_id", "t-1"),
					held(map[string]string{"brand": "v1"}, false),
				),
			},
			{
				Config: config(both, "", `labels = { brand = "v2" }`),
				Check:  held(map[string]string{"brand": "v2"}, true),
			},
			{
				// The console shortens the token lifetime: the plan puts
				// it back.
				PreConfig: func() {
					h.tenancy.editTenant("tenants/t-1", func(tn *corepb.Tenant) { tn.Config.AccessTokenExpireSeconds = 60 })
				},
				Config:             config(both, "", `labels = { brand = "v2" }`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config(both, "", `labels = { brand = "v2" }`),
				Check:  held(map[string]string{"brand": "v2"}, true),
			},
			{
				ResourceName:                         tenantConfigAddr,
				ImportState:                          true,
				ImportStateId:                        "t-1",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
			{
				ResourceName:                         tenantConfigAddr,
				ImportState:                          true,
				ImportStateId:                        "tenants/t-1",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			tn := h.tenancy.tenant("tenants/t-1")
			if !proto.Equal(tn.GetConfig(), &corepb.TenantConfig{}) || len(tn.GetLabels()) != 0 {
				return fmt.Errorf("destroy left config %v, labels %v", tn.GetConfig(), tn.GetLabels())
			}
			if tn.GetDisplayName() != seedTenant().GetDisplayName() {
				return fmt.Errorf("destroy changed display_name to %q", tn.GetDisplayName())
			}
			if n := len(h.fake.assets); n != 0 {
				return fmt.Errorf("%d assets left after destroy", n)
			}
			return nil
		},
	})
}

// TestAccTenantConfig_Refusals: kit's admissions, surfaced on config. A
// dark logo with no light one; a logo that is not an image; and an asset
// removed while the brand still names it, which is the failure the
// reference through the content avoids. Labels left unset are left alone.
func TestAccTenantConfig_Refusals(t *testing.T) {

	h := newHarness(t)
	h.tenancy.editTenant("tenants/t-1", func(tn *corepb.Tenant) { tn.Labels = map[string]string{"owner": "awtenant"} })

	text := h.providerConfig() + `
resource "authwise_asset" "terms" {
  display_name = "Terms"
  path         = "/terms.txt"
  mime_type    = "text/plain"
}
`
	logo := h.providerConfig() + `
resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.png"
  mime_type    = "image/png"
}
`
	tenantConfig := func(brand string) string {
		return fmt.Sprintf(`
resource "authwise_tenant_config" "this" {
  config = jsonencode({ branding = { %s } })
}
`, brand)
	}

	// The fresh fake's ids are sequential: the text asset is as-00000001,
	// the logo as-00000002. Step 4 names the logo by that literal.
	const logoID = "as-00000002"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      text + tenantConfig(`logoAssetId = authwise_asset.terms.asset_id`),
				ExpectError: regexp.MustCompile(`asset\s+as-\d+\s+is\s+text/plain,\s+not\s+an\s+image`),
			},
			{
				Config:      logo + tenantConfig(`logoDarkAssetId = authwise_asset.logo.asset_id`),
				ExpectError: regexp.MustCompile(`a\s+dark\s+variant\s+needs\s+the\s+logo\s+it\s+varies`),
			},
			{
				Config: logo + tenantConfig(`logo_asset_id = authwise_asset.logo.asset_id`),
				Check: func(s *terraform.State) error {
					if got := s.RootModule().Resources["authwise_asset.logo"].Primary.Attributes["asset_id"]; got != logoID {
						return fmt.Errorf("logo asset_id = %s", got)
					}
					return nil
				},
			},
			{
				// The asset goes, the brand still names it by a literal:
				// nothing orders the two, and kit refuses.
				Config:      h.providerConfig() + tenantConfig(`logoAssetId = "`+logoID+`"`),
				ExpectError: regexp.MustCompile(`asset\s+` + logoID + `\s+is\s+the\s+logo\s+of\s+tenants/t-1\s+\(config.branding.logo_asset_id\)`),
			},
			{
				Config: logo + tenantConfig(`logo_asset_id = authwise_asset.logo.asset_id`),
				Check: func(_ *terraform.State) error {
					if got := h.tenancy.tenant("tenants/t-1").GetLabels()["owner"]; got != "awtenant" {
						return fmt.Errorf("unmanaged labels changed: owner = %q", got)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if got := h.tenancy.tenant("tenants/t-1").GetLabels()["owner"]; got != "awtenant" {
				return fmt.Errorf("destroy cleared unmanaged labels: owner = %q", got)
			}
			return nil
		},
	})
}
