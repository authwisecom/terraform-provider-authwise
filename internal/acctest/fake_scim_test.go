package acctest_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Inbound SCIM's admin rows as apis v0.78.0 states them (kit#532, tf#18):
// a ScimConnection in a realm, its defaults filled by kit, and ScimTokens
// created only by CreateScimToken, whose secret is returned once as
// <id>_<plaintext>. A token has no update.

const (
	scimConnectionsSeg = "/scim-connections/"
	scimTokensSeg      = "/scim-tokens/"
	scimFakeIssuerHost = "https://id.acme.example"
)

var realmParent = regexp.MustCompile(`^tenants/[^/]+/realms/[^/]+$`)

// scimDefaultsLocked fills kit's defaults and checks the enumerations.
func scimDefaultsLocked(c *corepb.ScimConnection) error {
	if c.GetIdentifierAttribute() == "" {
		c.IdentifierAttribute = "userName"
	}
	if c.GetOnDelete() == "" {
		c.OnDelete = "deactivate"
	}
	if c.GetStatus() == "" {
		c.Status = "active"
	}
	switch c.GetIdentifierAttribute() {
	case "userName", "externalId", "emails.primary":
	default:
		return status.Errorf(codes.InvalidArgument, "identifier_attribute: userName, externalId or emails.primary, got %q", c.GetIdentifierAttribute())
	}
	switch c.GetOnDelete() {
	case "deactivate", "delete":
	default:
		return status.Errorf(codes.InvalidArgument, "on_delete: deactivate or delete, got %q", c.GetOnDelete())
	}
	switch c.GetStatus() {
	case "active", "disabled":
	default:
		return status.Errorf(codes.InvalidArgument, "status: active or disabled, got %q", c.GetStatus())
	}
	return nil
}

func (f *fakeIdentityServer) CreateScimConnection(ctx context.Context, in *identitypb.CreateScimConnectionRequest) (*corepb.ScimConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("scim connection", realmParent, in.GetParent()); err != nil {
		return nil, err
	}
	c := proto.Clone(in.GetScimConnection()).(*corepb.ScimConnection)
	if c.GetDisplayName() == "" || !strings.HasPrefix(c.GetIssuerId(), "i-") {
		return nil, status.Error(codes.InvalidArgument, "display_name and issuer_id (i-…) are required")
	}
	if err := scimDefaultsLocked(c); err != nil {
		return nil, err
	}
	id := f.nextID("sc")
	c.Name = in.GetParent() + scimConnectionsSeg + id
	c.BaseUrl = scimFakeIssuerHost + "/scim/" + id + "/v2"
	f.scimConnections[c.GetName()] = c
	return proto.Clone(c).(*corepb.ScimConnection), nil
}

func (f *fakeIdentityServer) GetScimConnection(ctx context.Context, in *identitypb.GetScimConnectionRequest) (*corepb.ScimConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c, ok := f.scimConnections[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "scim connection %q not found", in.GetName())
	}
	return proto.Clone(c).(*corepb.ScimConnection), nil
}

func (f *fakeIdentityServer) ListScimConnections(ctx context.Context, in *identitypb.ListScimConnectionsRequest) (*identitypb.ListScimConnectionsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	page, next := pageUnder(f.scimConnections, in.GetParent()+scimConnectionsSeg, in.GetPageToken())
	return &identitypb.ListScimConnectionsResponse{ScimConnections: page, NextPageToken: next}, nil
}

func (f *fakeIdentityServer) PatchScimConnection(ctx context.Context, in *identitypb.PatchScimConnectionRequest) (*corepb.ScimConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c, ok := f.scimConnections[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "scim connection %q not found", in.GetName())
	}
	next := proto.Clone(c).(*corepb.ScimConnection)
	src := in.GetScimConnection()
	for _, p := range in.GetUpdateMask().GetPaths() {
		switch p {
		case "display_name":
			next.DisplayName = src.GetDisplayName()
		case "labels":
			next.Labels = src.GetLabels()
		case "issuer_id":
			next.IssuerId = src.GetIssuerId()
		case "provider_id":
			next.ProviderId = src.GetProviderId()
		case "identifier_attribute":
			next.IdentifierAttribute = src.GetIdentifierAttribute()
		case "on_delete":
			next.OnDelete = src.GetOnDelete()
		case "status":
			next.Status = src.GetStatus()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: unknown path %q", p)
		}
	}
	if err := scimDefaultsLocked(next); err != nil {
		return nil, err
	}
	f.scimConnections[next.GetName()] = next
	return proto.Clone(next).(*corepb.ScimConnection), nil
}

// DeleteScimConnection takes the connection's tokens with it.
func (f *fakeIdentityServer) DeleteScimConnection(ctx context.Context, in *identitypb.DeleteScimConnectionRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.scimConnections[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "scim connection %q not found", in.GetName())
	}
	delete(f.scimConnections, in.GetName())
	id := in.GetName()[strings.LastIndex(in.GetName(), "/")+1:]
	for name, tok := range f.scimTokens {
		if tok.GetScimConnectionId() == id {
			delete(f.scimTokens, name)
		}
	}
	return &emptypb.Empty{}, nil
}

func (f *fakeIdentityServer) CreateScimToken(ctx context.Context, in *identitypb.CreateScimTokenRequest) (*identitypb.CreateScimTokenResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("scim token", realmParent, in.GetParent()); err != nil {
		return nil, err
	}
	if _, ok := f.scimConnections[in.GetParent()+scimConnectionsSeg+in.GetScimConnectionId()]; !ok {
		return nil, status.Errorf(codes.NotFound, "scim connection %q not found in %s", in.GetScimConnectionId(), in.GetParent())
	}
	if err := checkExpiry(in.GetExpiresAt()); err != nil {
		return nil, err
	}
	f.creates++
	id := f.nextID("st")
	tok := &corepb.ScimToken{
		Name:             in.GetParent() + scimTokensSeg + id,
		ScimConnectionId: in.GetScimConnectionId(),
		ExpiresAt:        in.GetExpiresAt(),
		CreatedAt:        timestamppb.Now(),
	}
	f.scimTokens[tok.GetName()] = tok
	return &identitypb.CreateScimTokenResponse{
		ScimToken: proto.Clone(tok).(*corepb.ScimToken),
		Secret:    fmt.Sprintf("%s_scim%d", id, f.creates),
	}, nil
}

func (f *fakeIdentityServer) GetScimToken(ctx context.Context, in *identitypb.GetScimTokenRequest) (*corepb.ScimToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	tok, ok := f.scimTokens[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "scim token %q not found", in.GetName())
	}
	return proto.Clone(tok).(*corepb.ScimToken), nil
}

func (f *fakeIdentityServer) ListScimTokens(ctx context.Context, in *identitypb.ListScimTokensRequest) (*identitypb.ListScimTokensResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	page, next := pageUnder(f.scimTokens, in.GetParent()+scimTokensSeg, in.GetPageToken())
	return &identitypb.ListScimTokensResponse{ScimTokens: page, NextPageToken: next}, nil
}

func (f *fakeIdentityServer) DeleteScimToken(ctx context.Context, in *identitypb.DeleteScimTokenRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.scimTokens[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "scim token %q not found", in.GetName())
	}
	delete(f.scimTokens, in.GetName())
	return &emptypb.Empty{}, nil
}
