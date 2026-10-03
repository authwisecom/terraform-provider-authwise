package acctest_test

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// --- User (realm-scoped) ---
//
// kit derives a user's email and phone from the account's proven
// identifiers and refuses all four contact fields on every write, whatever
// the value (kit#662, kit#666). The fake keeps the derived address apart
// from the row, in contact, which a test seeds the way an accepted
// invitation would.

var userContactFields = []string{"email", "email_verified", "phone_number", "phone_number_verified"}

// refuseUserContact is kit's write rule: a contact field set in the body,
// or named by the mask, is InvalidArgument.
func refuseUserContact(u *corepb.User, mask *fieldmaskpb.FieldMask) error {
	set := map[string]bool{
		"email":                 u.GetEmail() != "",
		"email_verified":        u.GetEmailVerified(),
		"phone_number":          u.GetPhoneNumber() != "",
		"phone_number_verified": u.GetPhoneNumberVerified(),
	}
	for _, p := range mask.GetPaths() {
		if _, ok := set[p]; ok {
			set[p] = true
		}
	}
	for _, field := range userContactFields {
		if set[field] {
			return status.Errorf(codes.InvalidArgument, "%s is output only; an address comes from an invitation", field)
		}
	}
	return nil
}

// readUserLocked is the row as kit returns it: the stored user with its
// derived address filled in.
func (f *fakeIdentityServer) readUserLocked(name string) *corepb.User {
	u := proto.Clone(f.users[name]).(*corepb.User)
	if email, ok := f.contact[name]; ok {
		u.Email = email
		u.EmailVerified = true
	}
	return u
}

func (f *fakeIdentityServer) CreateUser(ctx context.Context, in *identitypb.CreateUserRequest) (*corepb.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := refuseUserContact(in.GetUser(), nil); err != nil {
		return nil, err
	}
	u := proto.Clone(in.GetUser()).(*corepb.User)
	u.Name = in.GetParent() + "/users/" + f.nextID("u")
	f.users[u.GetName()] = u
	return f.readUserLocked(u.GetName()), nil
}

func (f *fakeIdentityServer) GetUser(ctx context.Context, in *identitypb.GetUserRequest) (*corepb.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.users[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "user %q not found", in.GetName())
	}
	return f.readUserLocked(in.GetName()), nil
}

func (f *fakeIdentityServer) PatchUser(ctx context.Context, in *identitypb.PatchUserRequest) (*corepb.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := refuseUserContact(in.GetUser(), in.GetUpdateMask()); err != nil {
		return nil, err
	}
	existing, ok := f.users[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "user %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "given_name":
			existing.GivenName = in.GetUser().GetGivenName()
		case "family_name":
			existing.FamilyName = in.GetUser().GetFamilyName()
		case "display_name":
			existing.DisplayName = in.GetUser().GetDisplayName()
		case "labels":
			existing.Labels = in.GetUser().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return f.readUserLocked(in.GetName()), nil
}

func (f *fakeIdentityServer) DeleteUser(ctx context.Context, in *identitypb.DeleteUserRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.users[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "user %q not found", in.GetName())
	}
	delete(f.users, in.GetName())
	delete(f.contact, in.GetName())
	return &emptypb.Empty{}, nil
}
