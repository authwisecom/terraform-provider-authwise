package acctest_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	accesspb "git.authwise.com/authwise/api-client-go/authwise/access/v1alpha1"
	identitypb "git.authwise.com/authwise/api-client-go/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/api-client-go/authwise/types/core/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeIdentityServer is an in-memory AIP server for the acceptance tier:
// real terraform CLI + the real provider + the real client_credentials
// exchange, with only the Authwise API replaced. It implements the
// resources the acceptance tests touch; everything else stays
// Unimplemented.
type fakeIdentityServer struct {
	identitypb.UnimplementedAuthwiseIdentityServiceServer

	mu        sync.Mutex
	realms    map[string]*corepb.Realm
	roles     map[string]*corepb.Role
	rolePerms map[string]map[string]bool
	audiences map[string]*corepb.Audience
	clients   map[string]*corepb.Client
	seq       int

	// lastAuthorization records the auth metadata of the most recent call
	// so tests can assert the bearer flow end to end.
	lastAuthorization string
}

func newFakeIdentityServer() *fakeIdentityServer {
	return &fakeIdentityServer{
		realms:    map[string]*corepb.Realm{},
		roles:     map[string]*corepb.Role{},
		rolePerms: map[string]map[string]bool{},
		audiences: map[string]*corepb.Audience{},
		clients:   map[string]*corepb.Client{},
	}
}

func (f *fakeIdentityServer) recordAuth(ctx context.Context) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("authorization"); len(v) > 0 {
			f.lastAuthorization = v[0]
		}
	}
}

func (f *fakeIdentityServer) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%08d", prefix, f.seq)
}

// --- Realm ---

func (f *fakeIdentityServer) GetRealm(ctx context.Context, in *identitypb.GetRealmRequest) (*corepb.Realm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	r, ok := f.realms[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	return proto.Clone(r).(*corepb.Realm), nil
}

func (f *fakeIdentityServer) ListRealms(ctx context.Context, in *identitypb.ListRealmsRequest) (*identitypb.ListRealmsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	res := &identitypb.ListRealmsResponse{}
	for name, r := range f.realms {
		if strings.HasPrefix(name, in.GetParent()+"/") {
			res.Realms = append(res.Realms, proto.Clone(r).(*corepb.Realm))
		}
	}
	return res, nil
}

func (f *fakeIdentityServer) CreateRealm(ctx context.Context, in *identitypb.CreateRealmRequest) (*corepb.Realm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	r := proto.Clone(in.GetRealm()).(*corepb.Realm)
	r.Name = in.GetParent() + "/realms/" + f.nextID("r")
	f.realms[r.GetName()] = r
	return proto.Clone(r).(*corepb.Realm), nil
}

func (f *fakeIdentityServer) PatchRealm(ctx context.Context, in *identitypb.PatchRealmRequest) (*corepb.Realm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.realms[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetRealm().GetDisplayName()
		case "config":
			existing.Config = in.GetRealm().GetConfig()
		case "labels":
			existing.Labels = in.GetRealm().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Realm), nil
}

func (f *fakeIdentityServer) DeleteRealm(ctx context.Context, in *identitypb.DeleteRealmRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.realms[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	delete(f.realms, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Role (audience-scoped: proves three-level parent composition) ---

func (f *fakeIdentityServer) GetRole(ctx context.Context, in *identitypb.GetRoleRequest) (*corepb.Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	r, ok := f.roles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "role %q not found", in.GetName())
	}
	return proto.Clone(r).(*corepb.Role), nil
}

func (f *fakeIdentityServer) CreateRole(ctx context.Context, in *identitypb.CreateRoleRequest) (*corepb.Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	r := proto.Clone(in.GetRole()).(*corepb.Role)
	r.Name = in.GetParent() + "/roles/" + f.nextID("ro")
	f.roles[r.GetName()] = r
	return proto.Clone(r).(*corepb.Role), nil
}

func (f *fakeIdentityServer) PatchRole(ctx context.Context, in *identitypb.PatchRoleRequest) (*corepb.Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.roles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "role %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetRole().GetDisplayName()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Role), nil
}

func (f *fakeIdentityServer) DeleteRole(ctx context.Context, in *identitypb.DeleteRoleRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.roles[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "role %q not found", in.GetName())
	}
	delete(f.roles, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Audience and Client (issuer-scoped; the console pair guard needs) ---

func (f *fakeIdentityServer) GetAudience(ctx context.Context, in *identitypb.GetAudienceRequest) (*corepb.Audience, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	a, ok := f.audiences[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "audience %q not found", in.GetName())
	}
	return proto.Clone(a).(*corepb.Audience), nil
}

func (f *fakeIdentityServer) CreateAudience(ctx context.Context, in *identitypb.CreateAudienceRequest) (*corepb.Audience, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	a := proto.Clone(in.GetAudience()).(*corepb.Audience)
	a.Name = in.GetParent() + "/audiences/" + f.nextID("a")
	f.audiences[a.GetName()] = a
	return proto.Clone(a).(*corepb.Audience), nil
}

func (f *fakeIdentityServer) PatchAudience(ctx context.Context, in *identitypb.PatchAudienceRequest) (*corepb.Audience, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.audiences[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "audience %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetAudience().GetDisplayName()
		case "labels":
			existing.Labels = in.GetAudience().GetLabels()
		case "config":
			existing.Config = in.GetAudience().GetConfig()
		case "appearance_profile_id":
			existing.AppearanceProfileId = in.GetAudience().GetAppearanceProfileId()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Audience), nil
}

func (f *fakeIdentityServer) DeleteAudience(ctx context.Context, in *identitypb.DeleteAudienceRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.audiences[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "audience %q not found", in.GetName())
	}
	delete(f.audiences, in.GetName())
	return &emptypb.Empty{}, nil
}

func (f *fakeIdentityServer) GetClient(ctx context.Context, in *identitypb.GetClientRequest) (*corepb.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c, ok := f.clients[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetName())
	}
	return proto.Clone(c).(*corepb.Client), nil
}

func (f *fakeIdentityServer) CreateClient(ctx context.Context, in *identitypb.CreateClientRequest) (*corepb.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c := proto.Clone(in.GetClient()).(*corepb.Client)
	c.Name = in.GetParent() + "/clients/" + f.nextID("c")
	f.clients[c.GetName()] = c
	return proto.Clone(c).(*corepb.Client), nil
}

func (f *fakeIdentityServer) PatchClient(ctx context.Context, in *identitypb.PatchClientRequest) (*corepb.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.clients[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetClient().GetDisplayName()
		case "audience_id":
			existing.AudienceId = in.GetClient().GetAudienceId()
		case "grant_type":
			existing.GrantType = in.GetClient().GetGrantType()
		case "config":
			existing.Config = in.GetClient().GetConfig()
		case "labels":
			existing.Labels = in.GetClient().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Client), nil
}

func (f *fakeIdentityServer) DeleteClient(ctx context.Context, in *identitypb.DeleteClientRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.clients[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetName())
	}
	delete(f.clients, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Role ↔ Permission association (the tf.Associate family) ---

// AssociatePermissionsToRole applies set/remove semantics over the role's
// permission set.
func (f *fakeIdentityServer) AssociatePermissionsToRole(ctx context.Context, in *identitypb.AssociatePermissionsToRoleRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.roles[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "role %q not found", in.GetName())
	}
	if f.rolePerms[in.GetName()] == nil {
		f.rolePerms[in.GetName()] = map[string]bool{}
	}
	for _, n := range in.GetAssociation().GetSet() {
		f.rolePerms[in.GetName()][n] = true
	}
	for _, n := range in.GetAssociation().GetRemove() {
		delete(f.rolePerms[in.GetName()], n)
	}
	return &emptypb.Empty{}, nil
}

// ListPermissionsByRole pages one permission at a time so the runtime's
// next-page-token walk is exercised by every refresh.
func (f *fakeIdentityServer) ListPermissionsByRole(ctx context.Context, in *identitypb.ListPermissionsByRoleRequest) (*identitypb.ListPermissionsByRoleResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.roles[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "role %q not found", in.GetName())
	}
	names := f.rolePermissionsLocked(in.GetName())
	start := 0
	if in.GetPageToken() != "" {
		fmt.Sscanf(in.GetPageToken(), "%d", &start)
	}
	res := &identitypb.ListPermissionsByRoleResponse{}
	if start < len(names) {
		res.Permissions = []*corepb.Permission{{Name: names[start]}}
		if start+1 < len(names) {
			res.NextPageToken = fmt.Sprintf("%d", start+1)
		}
	}
	return res, nil
}

// rolePermissionsLocked returns the role's permission names, sorted; the
// caller holds f.mu.
func (f *fakeIdentityServer) rolePermissionsLocked(role string) []string {
	names := make([]string, 0, len(f.rolePerms[role]))
	for n := range f.rolePerms[role] {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// rolePermissions is the test-facing variant of rolePermissionsLocked.
func (f *fakeIdentityServer) rolePermissions(role string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rolePermissionsLocked(role)
}

// addRolePermission seeds an out-of-band association.
func (f *fakeIdentityServer) addRolePermission(role, permission string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rolePerms[role] == nil {
		f.rolePerms[role] = map[string]bool{}
	}
	f.rolePerms[role][permission] = true
}

// harness boots the fake gRPC server plus a fake OAuth token endpoint and
// renders the provider block pointing at them.
type harness struct {
	fake        *fakeIdentityServer
	access      *fakeAccessServer
	grpcAddr    string
	tokenServer *httptest.Server
	tokenCalls  int
}

func newHarness(t *testing.T) *harness {

	t.Helper()

	h := &harness{fake: newFakeIdentityServer(), access: newFakeAccessServer()}

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.grpcAddr = lis.Addr().String()

	s := grpc.NewServer()
	identitypb.RegisterAuthwiseIdentityServiceServer(s, h.fake)
	accesspb.RegisterAuthwiseAccessServiceServer(s, h.access)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	h.tokenServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.tokenCalls++
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"access_token":"acc-test-token","expires_in":3600}`)
	}))
	t.Cleanup(h.tokenServer.Close)

	return h
}

// providerConfig renders the provider block for the fake stack. Scope
// defaults live at the provider level; per-resource overrides are part of
// the individual test configs.
func (h *harness) providerConfig() string {
	return fmt.Sprintf(`
provider "authwise" {
  endpoint      = %q
  insecure      = true
  token_url     = %q
  client_id     = "acc-client"
  client_secret = "acc-secret"
  audience      = "https://api.test"

  tenant_id   = "t-1"
  issuer_id   = "i-1"
  audience_id = "a-1"
}
`, h.grpcAddr, h.tokenServer.URL+"/oauth/token")
}
