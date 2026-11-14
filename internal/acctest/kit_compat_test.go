package acctest_test

import (
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAccKitCompat: against a kit without kit#968's names the provider
// refuses before its first call, naming the kit it needs and the one it
// found, instead of letting each renamed call answer Unimplemented. A check
// it cannot make does not block.
func TestAccKitCompat(t *testing.T) {

	cases := map[string]struct {
		capabilities func() (*identitypb.InstallCapabilities, error)
		want         *regexp.Regexp
	}{
		"kit 1.86 lists no identity.vocabulary": {
			capabilities: func() (*identitypb.InstallCapabilities, error) {
				return &identitypb.InstallCapabilities{Features: []string{"guard.tenant-collapse"}, KitVersion: "1.86.3"}, nil
			},
			want: regexp.MustCompile(`needs\s+kit\s+1\.87\.0\s+or\s+later[\s\S]*this\s+install\s+runs\s+kit\s+1\.86\.3`),
		},
		"a kit without GetInstallCapabilities": {
			capabilities: func() (*identitypb.InstallCapabilities, error) {
				return nil, status.Error(codes.Unimplemented, "unknown method")
			},
			want: regexp.MustCompile(`needs\s+kit\s+1\.87\.0\s+or\s+later[\s\S]*this\s+install\s+runs\s+an\s+older\s+kit`),
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
