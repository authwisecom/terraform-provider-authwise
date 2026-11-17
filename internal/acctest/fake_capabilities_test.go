package acctest_test

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
)

// GetInstallCapabilities answers as a kit with kit#968's names does, unless
// a test set capabilities to play another kit.
func (s *fakeIdentityServer) GetInstallCapabilities(_ context.Context, _ *identitypb.GetInstallCapabilitiesRequest) (*identitypb.InstallCapabilities, error) {
	if s.capabilities != nil {
		return s.capabilities()
	}
	return &identitypb.InstallCapabilities{
		Features:   []string{"guard.tenant-collapse", "guard.trusted-issuers", "identity.object-status", "identity.scim", "identity.vocabulary"},
		KitVersion: "1.91.0",
	}, nil
}
