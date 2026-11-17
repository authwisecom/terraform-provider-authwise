// tf#77: authwise_install_status reads kit's GetInstallStatus. An operator
// reads the license and registration; anyone else, and a kit that does not
// serve the call, gets a named refusal.
package acctest_test

import (
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const installStatusAddr = "data.authwise_install_status.this"

func (h *harness) installStatusConfig() string {
	return h.providerConfig() + `
data "authwise_install_status" "this" {}

output "registration" {
  value = data.authwise_install_status.this.registration.payload
}
`
}

func TestAccInstallStatus(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.installStatusConfig(),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(installStatusAddr, "name", "workplace"),
				resource.TestCheckResourceAttr(installStatusAddr, "id", "inst-1"),
				resource.TestCheckResourceAttr(installStatusAddr, "kit_version", "1.96.0"),
				resource.TestCheckResourceAttr(installStatusAddr, "license.state", "valid"),
				resource.TestCheckResourceAttr(installStatusAddr, "license.license_id", "lic-1"),
				resource.TestCheckResourceAttr(installStatusAddr, "license.exp", "2027-11-17T00:00:00Z"),
				resource.TestCheckResourceAttr(installStatusAddr, "license.keys.source", "embedded"),
				resource.TestCheckResourceAttr(installStatusAddr, "registration.enabled", "true"),
				resource.TestCheckResourceAttr(installStatusAddr, "registration.last_result", "ok"),
				resource.TestCheckOutput("registration", installPayload),
			),
		}},
	})
}

// An install with no license: its times are null, not the zero time, and
// it has no key set.
func TestAccInstallStatus_Unlicensed(t *testing.T) {

	h := newHarness(t)
	h.fake.installStatus = func() (*identitypb.InstallStatus, error) {
		return &identitypb.InstallStatus{
			Name:         "workplace",
			KitVersion:   "1.96.0",
			License:      &identitypb.InstallLicenseStatus{State: "absent"},
			Registration: &identitypb.InstallRegistrationStatus{},
		}, nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{{
			Config: h.installStatusConfig(),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(installStatusAddr, "license.state", "absent"),
				resource.TestCheckNoResourceAttr(installStatusAddr, "license.exp"),
				resource.TestCheckNoResourceAttr(installStatusAddr, "license.keys"),
				resource.TestCheckResourceAttr(installStatusAddr, "registration.enabled", "false"),
				resource.TestCheckNoResourceAttr(installStatusAddr, "registration.last_attempt_at"),
			),
		}},
	})
}

func TestAccInstallStatus_Refusals(t *testing.T) {

	cases := map[string]struct {
		err  error
		want *regexp.Regexp
	}{
		"not an operator": {
			err:  status.Error(codes.PermissionDenied, "identity.installStatus.get"),
			want: regexp.MustCompile(`(?s)only the install's operators may read the install\s+status`),
		},
		"an earlier kit": {
			err:  status.Error(codes.Unimplemented, "unknown method GetInstallStatus"),
			want: regexp.MustCompile(`(?s)needs kit\s+1\.94\.0 or later`),
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.fake.installStatus = func() (*identitypb.InstallStatus, error) { return nil, c.err }
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoFactories(),
				Steps:                    []resource.TestStep{{Config: h.installStatusConfig(), ExpectError: c.want}},
			})
		})
	}
}
