package provider

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc"
)

// userContactClient keeps a user's address fields off every user write.
//
// email, email_verified, phone_number and phone_number_verified are output
// only (kit#662, kit#666): kit derives them from the account's proven
// identifiers and refuses them on CreateUser, UpdateUser and PatchUser with
// InvalidArgument whatever the value, including a value it returned itself.
// The generated user resource marks them Computed, but its ToProto still
// copies state into the request, so an update of a user with an address
// would echo the address back and be refused. tfinfra has no Computed field
// that stays out of the request, so the fields are cleared here, on the way
// out.
type userContactClient struct {
	identitypb.AuthwiseIdentityServiceClient
}

func clearUserContact(u *corepb.User) {
	if u == nil {
		return
	}
	u.Email = ""
	u.EmailVerified = false
	u.PhoneNumber = ""
	u.PhoneNumberVerified = false
}

func (c userContactClient) CreateUser(ctx context.Context, in *identitypb.CreateUserRequest, opts ...grpc.CallOption) (*corepb.User, error) {
	clearUserContact(in.GetUser())
	return c.AuthwiseIdentityServiceClient.CreateUser(ctx, in, opts...)
}

func (c userContactClient) UpdateUser(ctx context.Context, in *identitypb.UpdateUserRequest, opts ...grpc.CallOption) (*corepb.User, error) {
	clearUserContact(in.GetUser())
	return c.AuthwiseIdentityServiceClient.UpdateUser(ctx, in, opts...)
}

func (c userContactClient) PatchUser(ctx context.Context, in *identitypb.PatchUserRequest, opts ...grpc.CallOption) (*corepb.User, error) {
	clearUserContact(in.GetUser())
	return c.AuthwiseIdentityServiceClient.PatchUser(ctx, in, opts...)
}
