package acctest_test

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
)

// GetInstallCapabilities answers as a kit before kit#968 does (1.86), unless
// a test set capabilities to play another kit.
func (s *fakeIdentityServer) GetInstallCapabilities(_ context.Context, _ *identitypb.GetInstallCapabilitiesRequest) (*identitypb.InstallCapabilities, error) {
	if s.capabilities != nil {
		return s.capabilities()
	}
	return &identitypb.InstallCapabilities{
		Features:   []string{"guard.tenant-collapse"},
		KitVersion: "1.86.3",
	}, nil
}
