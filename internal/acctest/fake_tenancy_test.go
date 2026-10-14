package acctest_test

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeTenancyServer is the public tenancy service's service accounts as
// kit#756 and kit#763 built them (apis v0.31.0): a client of the admin
// issuer the tenant owns, made with no secret and no roles; roles from a
// fixed list, set as a whole; secrets minted once, shown once, and
// listed live (unexpired) on the account; a rotating mint giving the
// others a bounded grace; and delete refused while any binding names the
// account. Bindings are the fake Access server's.
type fakeTenancyServer struct {
	tenancypb.UnimplementedAuthwiseTenancyServiceServer

	access   *fakeAccessServer
	identity *fakeIdentityServer

	mu       sync.Mutex
	accounts map[string]*tenancypb.ServiceAccount
	tenants  map[string]*corepb.Tenant
	mints    int
	roleSets int
	seq      int
}

func newFakeTenancyServer(access *fakeAccessServer, identity *fakeIdentityServer) *fakeTenancyServer {
	t := seedTenant()
	f := &fakeTenancyServer{
		access: access, identity: identity,
		accounts: map[string]*tenancypb.ServiceAccount{},
		tenants:  map[string]*corepb.Tenant{t.GetName(): t},
	}
	identity.brandHolding = f.brandHolding
	return f
}

var tenancyParent = regexp.MustCompile(`^tenants/[^/]+$`)

const (
	fakeAdminTokenURL = "https://central.example.com/t-01/i-01/oauth/token"
	fakeEstateAud     = "a-01"
	maxGrace          = 30 * 24 * time.Hour
)

func (f *fakeTenancyServer) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%08d", prefix, f.seq)
}

// liveLocked is the account as a read returns it: expired secrets dropped.
func (f *fakeTenancyServer) liveLocked(sa *tenancypb.ServiceAccount) *tenancypb.ServiceAccount {
	out := proto.Clone(sa).(*tenancypb.ServiceAccount)
	now := time.Now()
	out.Secrets = slices.DeleteFunc(out.GetSecrets(), func(s *tenancypb.ServiceAccountSecret) bool {
		return s.GetExpiresAt() != nil && !s.GetExpiresAt().AsTime().After(now)
	})
	return out
}

func (f *fakeTenancyServer) CreateServiceAccount(_ context.Context, in *tenancypb.CreateServiceAccountRequest) (*tenancypb.ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := checkParent("service account", tenancyParent, in.GetParent()); err != nil {
		return nil, err
	}
	name := in.GetServiceAccount().GetDisplayName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "service_account.display_name is required")
	}
	if err := checkExpiry(in.GetServiceAccount().GetExpiresAt()); err != nil {
		return nil, err
	}
	for k, sa := range f.accounts {
		if strings.HasPrefix(k, in.GetParent()+"/") && sa.GetDisplayName() == name {
			return nil, status.Errorf(codes.AlreadyExists, "a service account named %q already exists", name)
		}
	}
	id := f.nextID("c")
	now := timestamppb.Now()
	sa := &tenancypb.ServiceAccount{
		Name:        in.GetParent() + "/service-accounts/" + id,
		ClientId:    id,
		DisplayName: name,
		Status:      corepb.ClientStatus_CLIENT_STATUS_ACTIVE,
		ExpiresAt:   in.GetServiceAccount().GetExpiresAt(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.accounts[sa.GetName()] = sa
	return proto.Clone(sa).(*tenancypb.ServiceAccount), nil
}

func (f *fakeTenancyServer) GetServiceAccount(_ context.Context, in *tenancypb.GetServiceAccountRequest) (*tenancypb.ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.accounts[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "service account %q not found", in.GetName())
	}
	return f.liveLocked(sa), nil
}

func (f *fakeTenancyServer) ListServiceAccounts(_ context.Context, in *tenancypb.ListServiceAccountsRequest) (*tenancypb.ListServiceAccountsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	page, next := pageUnder(f.accounts, in.GetParent()+"/service-accounts/", in.GetPageToken())
	out := &tenancypb.ListServiceAccountsResponse{NextPageToken: next}
	for _, sa := range page {
		out.ServiceAccounts = append(out.ServiceAccounts, f.liveLocked(sa))
	}
	return out, nil
}

// DeleteServiceAccount refuses while any binding, at any anchor, names the
// account (SERVICE_ACCOUNTS §9.2), its own roles included.
func (f *fakeTenancyServer) DeleteServiceAccount(_ context.Context, in *tenancypb.DeleteServiceAccountRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.accounts[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "service account %q not found", in.GetName())
	}
	f.access.mu.Lock()
	var bound []string
	for name, b := range f.access.bindings {
		if b.GetSubjectId() == sa.GetClientId() {
			bound = append(bound, name)
		}
	}
	f.access.mu.Unlock()
	for _, role := range sa.GetRoles() {
		bound = append(bound, "role "+role)
	}
	if len(bound) > 0 {
		slices.Sort(bound)
		return nil, status.Errorf(codes.FailedPrecondition, "service account %s is still bound: %s", sa.GetClientId(), strings.Join(bound, ", "))
	}
	delete(f.accounts, in.GetName())
	return &emptypb.Empty{}, nil
}

func (f *fakeTenancyServer) MintServiceAccountSecret(_ context.Context, in *tenancypb.MintServiceAccountSecretRequest) (*tenancypb.MintServiceAccountSecretResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.accounts[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "service account %q not found", in.GetName())
	}
	if err := checkExpiry(in.GetExpiresAt()); err != nil {
		return nil, err
	}
	grace := 24 * time.Hour
	if in.GetGrace() != nil {
		grace = in.GetGrace().AsDuration()
	}
	if grace <= 0 || grace > maxGrace {
		return nil, status.Errorf(codes.InvalidArgument, "grace must be positive and at most 30 days, got %s", grace)
	}
	var rotated int32
	if in.GetRotate() {
		lapse := time.Now().Add(grace)
		for _, s := range f.liveLocked(sa).GetSecrets() {
			for _, stored := range sa.GetSecrets() {
				if stored.GetId() == s.GetId() && (stored.GetExpiresAt() == nil || stored.GetExpiresAt().AsTime().After(lapse)) {
					stored.ExpiresAt = timestamppb.New(lapse)
					rotated++
				}
			}
		}
	}
	f.mints++
	id := f.nextID("sas")
	sa.Secrets = append(sa.Secrets, &tenancypb.ServiceAccountSecret{Id: id, CreatedAt: timestamppb.Now(), ExpiresAt: in.GetExpiresAt()})
	return &tenancypb.MintServiceAccountSecretResponse{
		Secret:         fmt.Sprintf("%s_plaintext%d", id, f.mints),
		ServiceAccount: f.liveLocked(sa),
		TokenUrl:       fakeAdminTokenURL,
		Audience:       fakeEstateAud,
		RotatedSecrets: rotated,
	}, nil
}

// serviceAccountRoles are the roles a service account may hold (kit#763).
var serviceAccountRoles = map[string]bool{"tenant.admin": true, "tenant.viewer": true, "guardcontrol.admin": true}

func (f *fakeTenancyServer) SetServiceAccountRoles(_ context.Context, in *tenancypb.SetServiceAccountRolesRequest) (*tenancypb.ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.accounts[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "service account %q not found", in.GetName())
	}
	for _, role := range in.GetRoles() {
		if !serviceAccountRoles[role] {
			return nil, status.Errorf(codes.InvalidArgument, "role %q is not one a service account may hold", role)
		}
	}
	f.roleSets++
	roles := slices.Clone(in.GetRoles())
	slices.Sort(roles)
	sa.Roles = slices.Compact(roles)
	sa.UpdatedAt = timestamppb.Now()
	return f.liveLocked(sa), nil
}

// secret returns a stored secret by id, expired or not.
func (f *fakeTenancyServer) secret(id string) *tenancypb.ServiceAccountSecret {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sa := range f.accounts {
		for _, s := range sa.GetSecrets() {
			if s.GetId() == id {
				return proto.Clone(s).(*tenancypb.ServiceAccountSecret)
			}
		}
	}
	return nil
}

// expire ends a secret now, as its expiry or the end of a grace would.
func (f *fakeTenancyServer) expire(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sa := range f.accounts {
		for _, s := range sa.GetSecrets() {
			if s.GetId() == id {
				s.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			}
		}
	}
}
