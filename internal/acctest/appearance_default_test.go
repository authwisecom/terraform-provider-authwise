package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// An issuer's appearance is its default profile (kit#680, apis v0.18.0).

// profileBlock is one authwise_appearance_profile, with is_default = true
// when def.
func profileBlock(key, displayName string, def bool) string {
	isDefault := ""
	if def {
		isDefault = "\n  is_default   = true"
	}
	return fmt.Sprintf(`
resource "authwise_appearance_profile" %q {
  display_name = %q%s
}
`, key, displayName, isDefault)
}

// defaultIs checks that the issuer's only default on the server is the
// profile named by key's display name.
func defaultIs(h *harness, displayName string) resource.TestCheckFunc {
	return checkServer(func() error {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		var defaults []string
		for _, p := range h.fake.profiles {
			if p.GetIsDefault() {
				defaults = append(defaults, p.GetDisplayName())
			}
		}
		if len(defaults) != 1 || defaults[0] != displayName {
			return fmt.Errorf("defaults on the server = %v, want [%s]", defaults, displayName)
		}
		return nil
	})
}

func profilesGone(h *harness) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		if n := len(h.fake.profiles); n != 0 {
			return fmt.Errorf("%d appearance profiles left", n)
		}
		return nil
	}
}

// TestAccAppearanceProfile_Default: the first profile is the default
// without asking; the default moves by setting is_default on the new one and
// removing it from the old one, even while the old one changes in the same
// apply; and destroying them all succeeds whichever order Terraform picks,
// though kit refuses the default until it is the last.
func TestAccAppearanceProfile_Default(t *testing.T) {

	h := newHarness(t)
	cfg := func(blocks ...string) string { return h.providerConfig() + strings.Join(blocks, "") }

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg(profileBlock("a", "A", false)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_appearance_profile.a", "is_default", "true"),
					defaultIs(h, "A"),
				),
			},
			{
				Config: cfg(profileBlock("a", "A", false), profileBlock("b", "B", false), profileBlock("c", "C", false)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_appearance_profile.b", "is_default", "false"),
					resource.TestCheckResourceAttr("authwise_appearance_profile.c", "is_default", "false"),
					defaultIs(h, "A"),
				),
			},
			{
				// A changes in the same apply that moves the default off it,
				// and after it: the label's reference orders A's update behind
				// B's. Its plan must not promise a stale true.
				Config: cfg(`
resource "authwise_appearance_profile" "a" {
  display_name = "A renamed"
  labels       = { after = authwise_appearance_profile.b.name }
}
`, profileBlock("b", "B", true), profileBlock("c", "C", false)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_appearance_profile.b", "is_default", "true"),
					defaultIs(h, "B"),
				),
			},
			{
				Config: cfg(profileBlock("a", "A renamed", true), profileBlock("b", "B", false), profileBlock("c", "C", false)),
				Check:  defaultIs(h, "A renamed"),
			},
		},
		CheckDestroy: profilesGone(h),
	})
}

// TestAccAppearanceProfile_CreateAsDefault: a profile created with
// is_default = true beside an existing default takes over, through
// :makeDefault rather than the create kit would refuse.
func TestAccAppearanceProfile_CreateAsDefault(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.providerConfig() + profileBlock("a", "A", false),
				Check:  defaultIs(h, "A"),
			},
			{
				Config: h.providerConfig() + profileBlock("a", "A", false) + profileBlock("b", "B", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_appearance_profile.b", "is_default", "true"),
					defaultIs(h, "B"),
				),
			},
		},
		CheckDestroy: profilesGone(h),
	})
}

// TestAccAppearanceProfile_DefaultFalseRefused: is_default = false is
// refused at plan; kit could not honour it on an issuer's first profile.
func TestAccAppearanceProfile_DefaultFalseRefused(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.providerConfig() + `
resource "authwise_appearance_profile" "a" {
  display_name = "A"
  is_default   = false
}
`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`is_default can only be true`),
		}},
	})
}
