// authwise_asset_content (#28): the bytes behind an authwise_asset, uploaded
// through kit's UploadAsset stream. The steps walk what the hash has to
// catch: a first upload big enough to take several chunks, the same file
// path with new bytes, a change made on the server behind Terraform's back,
// and a switch from a file to inline content. Destroying the content alone
// removes the file and keeps the asset.
package acctest_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const assetContentAddr = "authwise_asset_content.logo"

func TestAccAssetContent_UploadDriftReplace(t *testing.T) {

	h := newHarness(t)

	src := filepath.Join(t.TempDir(), "logo.svg")
	write := func(b []byte) {
		if err := os.WriteFile(src, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Past the provider's 64 KiB chunk, so the upload takes several.
	first := bytes.Repeat([]byte("<svg/>"), 30_000)
	second := []byte("<svg>v2</svg>")
	inline := []byte("<svg>inline</svg>")
	write(first)

	asset := `
resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.svg"
  mime_type    = "image/svg+xml"
}
`
	fromFile := h.providerConfig() + asset + fmt.Sprintf(`
resource "authwise_asset_content" "logo" {
  asset_id = authwise_asset.logo.asset_id
  source   = %q
}
`, src)
	fromInline := h.providerConfig() + asset + fmt.Sprintf(`
resource "authwise_asset_content" "logo" {
  asset_id       = authwise_asset.logo.asset_id
  content_base64 = %q
}
`, base64.StdEncoding.EncodeToString(inline))

	// stored asserts the server holds want, and state records its hash.
	stored := func(want []byte, uploads int) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(assetContentAddr, "content_sha256", sha256Of(want)),
			func(s *terraform.State) error {
				name := s.RootModule().Resources["authwise_asset.logo"].Primary.Attributes["name"]
				got, ok := h.fake.blob(name)
				if !ok {
					return fmt.Errorf("asset %s has no content on the server", name)
				}
				if !bytes.Equal(got, want) {
					return fmt.Errorf("server holds %d bytes, want %d", len(got), len(want))
				}
				if n := h.fake.uploadCount(); n != uploads {
					return fmt.Errorf("%d uploads, want %d", n, uploads)
				}
				return nil
			},
		)
	}

	var assetName string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: fromFile,
				Check: resource.ComposeAggregateTestCheckFunc(
					stored(first, 1),
					func(s *terraform.State) error {
						assetName = s.RootModule().Resources["authwise_asset.logo"].Primary.Attributes["name"]
						return nil
					},
				),
			},
			// The same path with new bytes: the plan sees it through the
			// hash, and updates in place.
			{
				PreConfig: func() { write(second) },
				Config:    fromFile,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(assetContentAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: stored(second, 2),
			},
			// Changed on the server: the refresh shows drift, and the apply
			// puts the configured content back.
			{
				PreConfig: func() { h.fake.setBlob(assetName, []byte("tampered")) },
				Config:    fromFile,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(assetContentAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: stored(second, 3),
			},
			// Nothing changed: no upload.
			{
				Config: fromFile,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: stored(second, 3),
			},
			{
				Config: fromInline,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(assetContentAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: stored(inline, 4),
			},
			{
				ResourceName: assetContentAddr,
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return assetName, nil
				},
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "asset_id",
				ImportStateVerifyIgnore:              []string{"content_base64"},
			},
			{
				Config: h.providerConfig() + asset,
				Check: checkServer(func() error {
					if _, ok := h.fake.blob(assetName); ok {
						return fmt.Errorf("asset %s still has content after destroy", assetName)
					}
					if _, err := h.fake.GetAsset(t.Context(), &identitypb.GetAssetRequest{Name: assetName}); err != nil {
						return fmt.Errorf("the asset went with its content: %w", err)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccAssetContent_ExactlyOneSource(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.providerConfig() + `
resource "authwise_asset_content" "logo" {
  asset_id = "as-1"
}
`,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*source.*content_base64`),
			},
		},
	})
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
