package provider

import (
	"context"
	"net"
	"sync"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
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
	return &identitypb.InstallCapabilities{Features: k.features, KitVersion: "1.87.0"}, nil
}

func (k *capabilitiesKit) GetRealm(_ context.Context, req *identitypb.GetRealmRequest) (*corepb.Realm, error) {
	return &corepb.Realm{Name: req.GetName()}, nil
}

func compatClient(t *testing.T, kit *capabilitiesKit, tenant string) identitypb.AuthwiseIdentityServiceClient {
	t.Helper()
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := grpc.NewServer()
	identitypb.RegisterAuthwiseIdentityServiceServer(s, kit)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	compat := newKitCompat("0.34.1", tenant)
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
	kit := &capabilitiesKit{features: []string{"guard.tenant-collapse"}}
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
	kit := &capabilitiesKit{features: []string{"guard.tenant-collapse", featureVocabulary}}
	c := compatClient(t, kit, "t-1")
	for range 2 {
		_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		assert.Contains(t, err.Error(), "this install runs kit 1.87.0")
	}
	assert.Len(t, kit.parents, 1)
}

// TestKitCompat_TenantFromRequest: with no provider tenant, the check asks
// under the tenant the first request names.
func TestKitCompat_TenantFromRequest(t *testing.T) {
	kit := &capabilitiesKit{features: []string{"guard.tenant-collapse"}}
	c := compatClient(t, kit, "")
	_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-9/realms/r-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"tenants/t-9"}, kit.parents)
}

// TestKitCompat_PermissionDeniedLetsThrough: a check the credential may not
// make never blocks it.
// TestKitCompat_OlderKitLetsThrough: a kit with no GetInstallCapabilities
// predates kit#968, so this provider serves it.
func TestKitCompat_OlderKitLetsThrough(t *testing.T) {
	kit := &capabilitiesKit{err: status.Error(codes.Unimplemented, "unknown method")}
	c := compatClient(t, kit, "t-1")
	_, err := c.GetRealm(t.Context(), &identitypb.GetRealmRequest{Name: "tenants/t-1/realms/r-1"})
	require.NoError(t, err)
}

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
