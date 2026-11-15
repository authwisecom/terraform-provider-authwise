package acctest_test

import (
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAccKitCompat: against a kit with kit#968's names, this provider
// refuses before its first call, saying to upgrade to 0.35.0 and where the
// upgrade notes are, instead of letting each renamed call answer
// Unimplemented. Any kit before it, and a check it cannot make, pass.
func TestAccKitCompat(t *testing.T) {

	cases := map[string]struct {
		capabilities func() (*identitypb.InstallCapabilities, error)
		want         *regexp.Regexp
	}{
		"kit 1.87 lists identity.vocabulary": {
			capabilities: func() (*identitypb.InstallCapabilities, error) {
				return &identitypb.InstallCapabilities{Features: []string{"guard.tenant-collapse", "identity.vocabulary"}, KitVersion: "1.87.0"}, nil
			},
			want: regexp.MustCompile(`too\s+old\s+for\s+kit\s+1\.87\.0\s+or\s+later[\s\S]*this\s+install\s+runs\s+kit\s+1\.87\.0[\s\S]*Upgrade\s+to\s+provider\s+0\.35\.0\s+and\s+add\s+the\s+moved\s+blocks:\s+https://git\.authwise\.com/`),
		},
		"kit 1.86": {},
		"a kit without GetInstallCapabilities": {
			capabilities: func() (*identitypb.InstallCapabilities, error) {
				return nil, status.Error(codes.Unimplemented, "unknown method")
			},
		},
		"a credential that may not ask": {
			capabilities: func() (*identitypb.InstallCapabilities, error) {
				return nil, status.Error(codes.PermissionDenied, "PermissionDenied")
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.fake.capabilities = c.capabilities
			step := resource.TestStep{
				Config: h.providerConfig() + `
resource "authwise_realm" "x" {
  display_name = "Employees"
}
`,
			}
			if c.want != nil {
				step.ExpectError = c.want
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps:                    []resource.TestStep{step},
			})
			if c.want != nil && len(h.fake.realms) != 0 {
				t.Fatalf("the refused kit got a realm: %v", h.fake.realms)
			}
		})
	}
}
