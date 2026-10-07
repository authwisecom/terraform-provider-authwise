package acctest_test

import (
	"context"
	"fmt"
	"regexp"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ClientSecret, the kit#617 shape: minted rather than created, the secret
// returned once as <id>_<plaintext> beside a row that never carries it.
// expires_at is the only input, it must be in the future, and it patches
// in place without touching the credential.

var clientParent = regexp.MustCompile(`^tenants/[^/]+/issuers/[^/]+/clients/[^/]+$`)

func checkExpiry(ts *timestamppb.Timestamp) error {
	if ts != nil && !ts.AsTime().After(time.Now()) {
		return status.Errorf(codes.InvalidArgument, "expires_at must be in the future, got %s", ts.AsTime().Format(time.RFC3339))
	}
	return nil
}

func (f *fakeIdentityServer) MintClientSecret(ctx context.Context, in *identitypb.MintClientSecretRequest) (*identitypb.MintClientSecretResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("client secret", clientParent, in.GetParent()); err != nil {
		return nil, err
	}
	c, ok := f.clients[in.GetParent()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetParent())
	}
	if c.GetTokenEndpointAuthMethod() == authMethodNone {
		return nil, status.Errorf(codes.InvalidArgument,
			"client %s is public (token_endpoint_auth_method none) and holds no secret; "+
				"set token_endpoint_auth_method to client_secret_basic or client_secret_post first", in.GetParent())
	}
	if err := checkExpiry(in.GetExpiresAt()); err != nil {
		return nil, err
	}
	f.mints++
	id := f.nextID("cs")
	cs := &corepb.ClientSecret{
		Name:        in.GetParent() + "/client-secrets/" + id,
		HashEnabled: true,
		ExpiresAt:   in.GetExpiresAt(),
	}
	f.clientSecrets[cs.GetName()] = cs
	return &identitypb.MintClientSecretResponse{
		ClientSecret: proto.Clone(cs).(*corepb.ClientSecret),
		Secret:       fmt.Sprintf("%s_plaintext%d", id, f.mints),
	}, nil
}

func (f *fakeIdentityServer) GetClientSecret(ctx context.Context, in *identitypb.GetClientSecretRequest) (*corepb.ClientSecret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	cs, ok := f.clientSecrets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client secret %q not found", in.GetName())
	}
	return proto.Clone(cs).(*corepb.ClientSecret), nil
}

func (f *fakeIdentityServer) ListClientSecrets(ctx context.Context, in *identitypb.ListClientSecretsRequest) (*identitypb.ListClientSecretsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	page, next := pageUnder(f.clientSecrets, in.GetParent()+"/client-secrets/", in.GetPageToken())
	return &identitypb.ListClientSecretsResponse{ClientSecrets: page, NextPageToken: next}, nil
}

func (f *fakeIdentityServer) PatchClientSecret(ctx context.Context, in *identitypb.PatchClientSecretRequest) (*corepb.ClientSecret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.clientSecrets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client secret %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "expires_at":
			if err := checkExpiry(in.GetClientSecret().GetExpiresAt()); err != nil {
				return nil, err
			}
			existing.ExpiresAt = in.GetClientSecret().GetExpiresAt()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.ClientSecret), nil
}

func (f *fakeIdentityServer) DeleteClientSecret(ctx context.Context, in *identitypb.DeleteClientSecretRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.clientSecrets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "client secret %q not found", in.GetName())
	}
	delete(f.clientSecrets, in.GetName())
	return &emptypb.Empty{}, nil
}
