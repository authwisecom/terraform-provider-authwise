package provider

import (
	"context"
	"testing"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// stubIdentity records what reached kit, for the appearance-default client.
type stubIdentity struct {
	identitypb.AuthwiseIdentityServiceClient

	isDefault   bool
	deletesTil  int // DeleteAppearanceProfile refuses until this many calls
	deletes     int
	patches     []*identitypb.PatchAppearanceProfileRequest
	gets        int
	madeDefault []string
	// siblings are the issuer's other profiles. With refuseWhileDefault,
	// kit refuses deleting this one while it is the default and has them.
	siblings           []string
	refuseWhileDefault bool
	lists              int
}

func (s *stubIdentity) ListAppearanceProfiles(_ context.Context, _ *identitypb.ListAppearanceProfilesRequest, _ ...grpc.CallOption) (*identitypb.ListAppearanceProfilesResponse, error) {
	s.lists++
	out := &identitypb.ListAppearanceProfilesResponse{AppearanceProfiles: []*corepb.AppearanceProfile{{Name: profileName, IsDefault: s.isDefault}}}
	for _, n := range s.siblings {
		out.AppearanceProfiles = append(out.AppearanceProfiles, &corepb.AppearanceProfile{Name: n})
	}
	return out, nil
}

func (s *stubIdentity) GetAppearanceProfile(_ context.Context, in *identitypb.GetAppearanceProfileRequest, _ ...grpc.CallOption) (*corepb.AppearanceProfile, error) {
	s.gets++
	return &corepb.AppearanceProfile{Name: in.GetName(), IsDefault: s.isDefault}, nil
}

func (s *stubIdentity) PatchAppearanceProfile(_ context.Context, in *identitypb.PatchAppearanceProfileRequest, _ ...grpc.CallOption) (*corepb.AppearanceProfile, error) {
	s.patches = append(s.patches, in)
	return &corepb.AppearanceProfile{Name: in.GetName(), DisplayName: in.GetAppearanceProfile().GetDisplayName(), IsDefault: s.isDefault}, nil
}

func (s *stubIdentity) MakeDefaultAppearanceProfile(_ context.Context, in *identitypb.MakeDefaultAppearanceProfileRequest, _ ...grpc.CallOption) (*identitypb.MakeDefaultAppearanceProfileResponse, error) {
	s.madeDefault = append(s.madeDefault, in.GetName())
	s.isDefault = in.GetName() == profileName
	return &identitypb.MakeDefaultAppearanceProfileResponse{Default: &corepb.AppearanceProfile{Name: in.GetName(), IsDefault: true}}, nil
}

func (s *stubIdentity) DeleteAppearanceProfile(_ context.Context, _ *identitypb.DeleteAppearanceProfileRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	s.deletes++
	if s.deletes < s.deletesTil || (s.refuseWhileDefault && s.isDefault && len(s.siblings) > 0) {
		return nil, status.Error(codes.FailedPrecondition, "the issuer's default")
	}
	return &emptypb.Empty{}, nil
}

const profileName = "tenants/t-1/issuers/i-1/appearance-profiles/ap-1"

func stubbed(s *stubIdentity, wait time.Duration) appearanceDefaultClient {
	return appearanceDefaultClient{AuthwiseIdentityServiceClient: s, deleteWait: wait, poll: time.Millisecond}
}

// TestAppearanceDefault_PatchNeverWritesTheFlag: no write carries
// is_default, in the body or the mask (the kit#662 echo trap); true goes
// through :makeDefault; and a patch of the flag alone reads rather than
// sending an empty mask, which AIP takes as a full update.
func TestAppearanceDefault_PatchNeverWritesTheFlag(t *testing.T) {

	type c struct {
		mask         []string
		isDefault    bool
		wantPatched  []string // the mask kit received; nil for no patch
		wantGet      bool
		wantDefaults int
	}

	cases := map[string]c{
		"stale true echoed, flag not masked": {mask: []string{displayName}, isDefault: true, wantPatched: []string{displayName}},
		"true with another field":            {mask: []string{displayName, isDefaultAttr}, isDefault: true, wantPatched: []string{displayName}, wantDefaults: 1},
		"true alone":                         {mask: []string{isDefaultAttr}, isDefault: true, wantGet: true, wantDefaults: 1},
		"unknown planned, sent as false":     {mask: []string{displayName, isDefaultAttr}, wantPatched: []string{displayName}},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			s := &stubIdentity{}
			out, err := stubbed(s, 0).PatchAppearanceProfile(context.Background(), &identitypb.PatchAppearanceProfileRequest{
				Name:              profileName,
				AppearanceProfile: &corepb.AppearanceProfile{Name: profileName, DisplayName: "Brand", IsDefault: v.isDefault},
				UpdateMask:        &fieldmaskpb.FieldMask{Paths: v.mask},
			})
			require.NoError(t, err)
			if v.wantPatched == nil {
				assert.Empty(t, s.patches)
			} else {
				require.Len(t, s.patches, 1)
				assert.Equal(t, v.wantPatched, s.patches[0].GetUpdateMask().GetPaths())
				assert.False(t, s.patches[0].GetAppearanceProfile().GetIsDefault(), "the body carried is_default")
			}
			assert.Equal(t, v.wantGet, s.gets > 0)
			assert.Len(t, s.madeDefault, v.wantDefaults)
			assert.Equal(t, v.wantDefaults > 0, out.GetIsDefault())
		})
	}
}

// TestAppearanceDefault_DeleteWaitsForSiblings: deleting the default is
// retried while kit refuses it, and gives up with the reason once the wait
// runs out.
func TestAppearanceDefault_DeleteWaitsForSiblings(t *testing.T) {

	ctx := context.Background()
	req := &identitypb.DeleteAppearanceProfileRequest{Name: profileName}

	t.Run("siblings go first", func(t *testing.T) {
		s := &stubIdentity{isDefault: true, deletesTil: 3}
		_, err := stubbed(s, time.Second).DeleteAppearanceProfile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, 3, s.deletes)
	})

	t.Run("siblings stay", func(t *testing.T) {
		s := &stubIdentity{isDefault: true, deletesTil: 1 << 30}
		_, err := stubbed(s, 20*time.Millisecond).DeleteAppearanceProfile(ctx, req)
		require.Error(t, err)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		assert.Contains(t, err.Error(), "is_default = true on the profile that should take over")
	})

	t.Run("not the default: no retry", func(t *testing.T) {
		s := &stubIdentity{deletesTil: 1 << 30}
		_, err := stubbed(s, time.Second).DeleteAppearanceProfile(ctx, req)
		require.Error(t, err)
		assert.Equal(t, 1, s.deletes)
	})
}

// TestAppearanceDefault_DeleteHandsOffToTheOneLeft (tf#75, kit#953): a new
// issuer of a branded tenant has kit's own "Default" profile; a
// configuration that made its profile the default could not be destroyed,
// since kit refuses deleting the default while that one stays. With one
// other profile left, the default is handed to it and the delete goes
// through at once; with several, nothing is handed and the wait ends in the
// refusal.
func TestAppearanceDefault_DeleteHandsOffToTheOneLeft(t *testing.T) {

	ctx := context.Background()
	req := &identitypb.DeleteAppearanceProfileRequest{Name: profileName}
	kitDefault := "tenants/t-1/issuers/i-1/appearance-profiles/ap-kit"

	t.Run("one other", func(t *testing.T) {
		s := &stubIdentity{isDefault: true, refuseWhileDefault: true, siblings: []string{kitDefault}}
		_, err := stubbed(s, time.Minute).DeleteAppearanceProfile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, []string{kitDefault}, s.madeDefault)
		assert.Equal(t, 2, s.deletes)
	})

	t.Run("several others", func(t *testing.T) {
		s := &stubIdentity{isDefault: true, refuseWhileDefault: true, siblings: []string{kitDefault, kitDefault + "2"}}
		_, err := stubbed(s, 20*time.Millisecond).DeleteAppearanceProfile(ctx, req)
		require.Error(t, err)
		assert.Empty(t, s.madeDefault)
		assert.Contains(t, err.Error(), "is_default = true on the profile that should take over")
	})
}
