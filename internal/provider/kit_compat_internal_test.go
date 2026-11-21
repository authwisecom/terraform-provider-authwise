package provider

import (
	"context"
	"net"
	"sync"
	"testing"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// capabilitiesKit counts the check's questions and the tenants they name.
type capabilitiesKit struct {
	identitypb.UnimplementedAuthwiseIdentityServiceServer
	features []string
	err      error

	mu      sync.Mutex
	parents []string
}

func (k *capabilitiesKit) GetInstallCapabilities(_ context.Context, req *identitypb.GetInstallCapabilitiesRequest) (*identitypb.InstallCapabilities, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.parents = append(k.parents, req.GetParent())
	if k.err != nil {
		return nil, k.err
	}
	return &identitypb.InstallCapabilities{Features: k.features, KitVersion: "1.86.3"}, nil
}

func (k *capabilitiesKit) GetRealm(_ context.Context, req *identitypb.GetRealmRequest) (*corepb.Realm, error) {
	return &corepb.Realm{Name: req.GetName()}, nil
}

func (k *capabilitiesKit) GetScimConnection(_ context.Context, req *identitypb.GetScimConnectionRequest) (*corepb.ScimConnection, error) {
	return &corepb.ScimConnection{Name: req.GetName()}, nil
}

func compatClient(t *testing.T, kit *capabilitiesKit, tenant string) identitypb.AuthwiseIdentityServiceClient {
	t.Helper()
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := grpc.NewServer()
	identitypb.RegisterAuthwiseIdentityServiceServer(s, kit)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	compat := newKitCompat("0.35.0", tenant)
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(compat.unary()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return identitypb.NewAuthwiseIdentityServiceClient(conn)
}

// TestKitCompat_AsksOnce: one question per provider run, under the
// provider's tenant, however many calls follow.
func TestKitCompat_AsksOnce(t *testing.T) {
	kit := &capabilitiesKit{features: []string{featureVocabulary}}
	c := compatClient(t, kit, "t-1")
	for range 3 {
		_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"})
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"tenants/t-1"}, kit.parents)
}

// TestKitCompat_RefusesOnce: the refusal is remembered; kit is not asked
// again and the call never reaches it.
func TestKitCompat_RefusesOnce(t *testing.T) {
	kit := &capabilitiesKit{features: []string{"guard.tenant-collapse"}}
	c := compatClient(t, kit, "t-1")
	for range 2 {
		_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		assert.Contains(t, err.Error(), "this install runs kit 1.86.3")
	}
	assert.Len(t, kit.parents, 1)
}

// TestKitCompat_TenantFromRequest: with no provider tenant, the check asks
// under the tenant the first request names.
func TestKitCompat_TenantFromRequest(t *testing.T) {
	kit := &capabilitiesKit{features: []string{featureVocabulary}}
	c := compatClient(t, kit, "")
	_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-9/realms/r-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"tenants/t-9"}, kit.parents)
}

// TestKitCompat_PermissionDeniedLetsThrough: a check the credential may not
// make never blocks it.
func TestKitCompat_PermissionDeniedLetsThrough(t *testing.T) {
	kit := &capabilitiesKit{err: status.Error(codes.PermissionDenied, "PermissionDenied")}
	c := compatClient(t, kit, "t-1")
	for range 2 {
		_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"})
		require.NoError(t, err)
	}
	assert.Len(t, kit.parents, 1)
}

func TestTenantOf(t *testing.T) {
	assert.Equal(t, "t-1", tenantOf(&identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"}))
	assert.Equal(t, "t-2", tenantOf(&identitypb.ListRealmsRequest{Parent: "tenants/t-2"}))
	assert.Empty(t, tenantOf(&identitypb.GetRealmRequest{Name: "realms/r-1"}))
	assert.Empty(t, tenantOf(nil))
}

// TestKitCompat_GatedFeature: a SCIM call needs identity.scim; without it
// the call is refused by name and never reaches kit, while the rest of the
// provider keeps working against that kit.
func TestKitCompat_GatedFeature(t *testing.T) {
	scim := &identitypb.GetScimConnectionRequest{Name: "tenants/t-1/realms/r-1/scim-connections/sc-1"}

	kit := &capabilitiesKit{features: []string{featureVocabulary}}
	c := compatClient(t, kit, "t-1")
	_, err := c.GetScimConnection(t.Context(), scim)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "identity.scim")
	assert.Contains(t, err.Error(), "authwise_scim_connection")
	assert.Contains(t, err.Error(), "kit 1.86.3")
	_, err = c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"})
	require.NoError(t, err)
	assert.Len(t, kit.parents, 1)

	kit = &capabilitiesKit{features: []string{featureVocabulary, "identity.scim"}}
	c = compatClient(t, kit, "t-1")
	_, err = c.GetScimConnection(t.Context(), scim)
	require.NoError(t, err)
}

// TestKitCompat_GatedUnanswered: a check that could not be made refuses no
// gated call either.
func TestKitCompat_GatedUnanswered(t *testing.T) {
	kit := &capabilitiesKit{err: status.Error(codes.PermissionDenied, "PermissionDenied")}
	c := compatClient(t, kit, "t-1")
	_, err := c.GetScimConnection(t.Context(), &identitypb.GetScimConnectionRequest{Name: "tenants/t-1/realms/r-1/scim-connections/sc-1"})
	require.NoError(t, err)
}

// TestKitCompat_GatedUnaryGuard: on guard-control's connection only the
// trusted issuer calls are checked, and the question goes to kit.
func TestKitCompat_GatedUnaryGuard(t *testing.T) {
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	kit := &capabilitiesKit{features: []string{featureVocabulary}}
	s := grpc.NewServer()
	identitypb.RegisterAuthwiseIdentityServiceServer(s, kit)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	kitConn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = kitConn.Close() })

	gated := newKitCompat("0.35.0", "t-1").gatedUnary(kitConn)
	invoked := 0
	invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		invoked++
		return nil
	}

	err = gated(t.Context(), guardpb.GuardControlService_GetNetwork_FullMethodName,
		&guardpb.GetNetworkRequest{Name: "tenants/t-1/networks/n-1"}, nil, nil, invoker)
	require.NoError(t, err)
	assert.Empty(t, kit.parents, "an ungated guard call asks kit nothing")

	err = gated(t.Context(), guardpb.GuardTrustedIssuerService_GetTrustedIssuer_FullMethodName,
		&guardpb.GetTrustedIssuerRequest{Name: "tenants/t-1/networks/n-1/trustedIssuers/ti-1"}, nil, nil, invoker)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "guard.trusted-issuers")
	assert.Equal(t, 1, invoked)
}

// TestKitCompat_StatusWrites: on a kit without identity.object-status, a
// write that sets status, in its object or its mask, is refused by name;
// the same write without status, and every read, reaches kit.
func TestKitCompat_StatusWrites(t *testing.T) {
	kit := &capabilitiesKit{features: []string{featureVocabulary}}
	c := compatClient(t, kit, "t-1")
	realm := "tenants/t-1/realms/r-1"

	_, err := c.PatchRealm(t.Context(), &identitypb.PatchRealmRequest{
		Name: realm, Realm: &corepb.Realm{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{statusField}},
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "identity.object-status")

	_, err = c.CreateIssuer(t.Context(), &identitypb.CreateIssuerRequest{
		Parent: "tenants/t-1", Issuer: &corepb.Issuer{Status: "disabled"},
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	// No status: kit is reached (this fake answers Unimplemented).
	_, err = c.PatchRealm(t.Context(), &identitypb.PatchRealmRequest{
		Name: realm, Realm: &corepb.Realm{DisplayName: "x"}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{displayName}},
	})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: realm})
	require.NoError(t, err)

	kit = &capabilitiesKit{features: []string{featureVocabulary, "identity.object-status"}}
	c = compatClient(t, kit, "t-1")
	_, err = c.PatchRealm(t.Context(), &identitypb.PatchRealmRequest{
		Name: realm, Realm: &corepb.Realm{Status: "disabled"}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{statusField}},
	})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

// TestKitCompat_ProviderUpstreamTrust: on a kit without
// identity.provider-upstream-trust, a provider write that sets
// trusted_email_domains, in its object or its mask, is refused by name; a
// provider write without it reaches kit.
func TestKitCompat_ProviderUpstreamTrust(t *testing.T) {
	kit := &capabilitiesKit{features: []string{featureVocabulary}}
	c := compatClient(t, kit, "t-1")
	parent := "tenants/t-1/realms/r-1"
	saml := &corepb.Provider{ProviderType: "saml", TrustedEmailDomains: []string{"contoso.com"}}

	_, err := c.CreateProvider(t.Context(), &identitypb.CreateProviderRequest{Parent: parent, Provider: saml})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "identity.provider-upstream-trust")

	_, err = c.PatchProvider(t.Context(), &identitypb.PatchProviderRequest{
		Name: parent + "/providers/p-1", Provider: &corepb.Provider{},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{trustedEmailDomainsField}},
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	// No list: kit is reached (this fake answers Unimplemented).
	_, err = c.CreateProvider(t.Context(), &identitypb.CreateProviderRequest{Parent: parent, Provider: &corepb.Provider{ProviderType: "saml"}})
	assert.Equal(t, codes.Unimplemented, status.Code(err))

	kit = &capabilitiesKit{features: []string{featureVocabulary, providerUpstreamTrust.feature}}
	c = compatClient(t, kit, "t-1")
	_, err = c.CreateProvider(t.Context(), &identitypb.CreateProviderRequest{Parent: parent, Provider: saml})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}
