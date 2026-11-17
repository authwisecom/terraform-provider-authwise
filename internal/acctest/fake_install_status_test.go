package acctest_test

import (
	"context"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// installPayload is the registration body the fake install would send.
const installPayload = `{"schema":"authwise.install-registration/v1","install_id":"inst-1","license_id":"lic-1"}`

// GetInstallStatus answers as kit 1.95.0 does for an operator of a
// licensed, registered install, unless a test set installStatus.
func (s *fakeIdentityServer) GetInstallStatus(_ context.Context, in *identitypb.GetInstallStatusRequest) (*identitypb.InstallStatus, error) {
	if in.GetParent() != "tenants/-" {
		return nil, status.Errorf(codes.InvalidArgument, "parent: only tenants/- (the install), not %q", in.GetParent())
	}
	if s.installStatus != nil {
		return s.installStatus()
	}
	checked := timestamppb.New(time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
	return &identitypb.InstallStatus{
		Name:       "workplace",
		Id:         "inst-1",
		Url:        "https://id.workplace.example",
		KitVersion: "1.96.0",
		License: &identitypb.InstallLicenseStatus{
			State:      "valid",
			LicenseId:  "lic-1",
			Org:        "org-1",
			Plan:       "FREE",
			Exp:        timestamppb.New(time.Date(2027, 11, 17, 0, 0, 0, 0, time.UTC)),
			GraceUntil: timestamppb.New(time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC)),
			CheckedAt:  checked,
			Keys:       &identitypb.InstallLicenseKeys{Source: "embedded", UpdatedAt: checked},
		},
		Registration: &identitypb.InstallRegistrationStatus{
			Enabled:       true,
			LastAttemptAt: checked,
			LastResult:    "ok",
			Payload:       installPayload,
		},
	}, nil
}
