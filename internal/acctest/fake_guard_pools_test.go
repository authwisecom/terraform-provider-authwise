package acctest_test

import (
	"context"
	"fmt"
	"slices"
	"strings"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Estate relay pools (guard-control#81, tf#66): pools and their relays at
// the estate's root, each tenant's access, and relays' credentials. The
// rules are guard-control's docswip/RELAY_POOLS.md as apis v0.59.0 states
// them; names are kit's kebab collections.

const (
	relayPoolsPrefix = "relay-pools/"
	poolRelaysPrefix = "pool-relays/"
)

func (f *fakeGuardServer) tenantDefaultPoolLocked(tenant string) string {
	if a, ok := f.access[tenant]; ok && a.GetDefaultPool() != "" {
		return a.GetDefaultPool()
	}
	for _, p := range sortedValues(f.pools) {
		if p.GetDefault() {
			return p.GetName()
		}
	}
	return ""
}

// allowedPoolsLocked is the pool names a tenant may use: the estate
// default, its own default and those an operator allowed it.
func (f *fakeGuardServer) allowedPoolsLocked(tenant string) []string {
	var out []string
	for _, p := range sortedValues(f.pools) {
		if p.GetDefault() {
			out = append(out, p.GetName())
		}
	}
	if a, ok := f.access[tenant]; ok {
		if a.GetDefaultPool() != "" {
			out = append(out, a.GetDefaultPool())
		}
		out = append(out, a.GetAllowedPools()...)
	}
	return out
}

func (f *fakeGuardServer) networkHasRelayLocked(network string) bool {
	return network != "" && len(children(f.relays, network+"/relays/")) > 0
}

// checkNetworkPoolLocked applies a network's pool rules: empty on create is
// the tenant's default, stored; the pool must be allowed, by name (a slug
// is not resolved); pool_fallback needs a relay of the network's own.
func (f *fakeGuardServer) checkNetworkPoolLocked(tenant, network string, n *guardpb.Network, create bool) error {
	if n.GetRelayPool() == "" && create {
		n.RelayPool = f.tenantDefaultPoolLocked(tenant)
	}
	if n.GetRelayPool() != "" {
		if !slices.Contains(f.allowedPoolsLocked(tenant), n.GetRelayPool()) {
			return guardRefusal(codes.FailedPrecondition, "RELAY_POOL_NOT_ALLOWED",
				fmt.Sprintf("relay pool %s is not allowed for tenant %s", n.GetRelayPool(), tenant))
		}
	}
	if n.GetPoolFallback() && !f.networkHasRelayLocked(network) {
		return guardRefusal(codes.FailedPrecondition, "POOL_FALLBACK_WITHOUT_RELAY",
			"pool_fallback needs a relay of the network's own")
	}
	return nil
}

// --- RelayPool ---

func checkRelayPool(p *guardpb.RelayPool) error {
	switch p.GetSelect() {
	case "served-order", "nearest":
	default:
		return status.Errorf(codes.InvalidArgument, "select %q must be served-order or nearest", p.GetSelect())
	}
	return nil
}

func (f *fakeGuardServer) setDefaultPoolLocked(p *guardpb.RelayPool) {
	if !p.GetDefault() {
		return
	}
	for _, other := range f.pools {
		if other.GetName() != p.GetName() {
			other.Default = false
		}
	}
}

func (f *fakeGuardServer) poolOut(p *guardpb.RelayPool) *guardpb.RelayPool {
	out := proto.Clone(p).(*guardpb.RelayPool)
	var n int32
	for _, r := range f.poolRelays {
		if r.GetPool() == p.GetName() {
			n++
		}
	}
	out.RelayCount = n
	return out
}

func (f *fakeGuardServer) CreateRelayPool(ctx context.Context, in *guardpb.CreateRelayPoolRequest) (*guardpb.RelayPool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	p := proto.Clone(in.GetRelayPool()).(*guardpb.RelayPool)
	if p.GetSelect() == "" {
		p.Select = "served-order"
	}
	if err := checkRelayPool(p); err != nil {
		return nil, err
	}
	for _, other := range f.pools {
		if p.GetSlug() != "" && other.GetSlug() == p.GetSlug() {
			return nil, status.Errorf(codes.AlreadyExists, "relay pool slug %q is taken", p.GetSlug())
		}
	}
	p.Name = relayPoolsPrefix + f.nextID("rp")
	f.pools[p.GetName()] = p
	f.setDefaultPoolLocked(p)
	return f.poolOut(p), nil
}

func (f *fakeGuardServer) GetRelayPool(ctx context.Context, in *guardpb.GetRelayPoolRequest) (*guardpb.RelayPool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	p, ok := f.pools[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "relay pool %q not found", in.GetName())
	}
	return f.poolOut(p), nil
}

func (f *fakeGuardServer) ListRelayPools(ctx context.Context, _ *guardpb.ListRelayPoolsRequest) (*guardpb.ListRelayPoolsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListRelayPoolsResponse{}
	for _, p := range sortedValues(f.pools) {
		out.RelayPools = append(out.RelayPools, f.poolOut(p))
	}
	return out, nil
}

func (f *fakeGuardServer) PatchRelayPool(ctx context.Context, in *guardpb.PatchRelayPoolRequest) (*guardpb.RelayPool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	p, ok := f.pools[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "relay pool %q not found", in.GetName())
	}
	next := proto.Clone(p).(*guardpb.RelayPool)
	if err := applyMask(next, in.GetRelayPool(), in.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	next.RelayCount = 0
	if err := checkRelayPool(next); err != nil {
		return nil, err
	}
	f.pools[in.GetName()] = next
	f.setDefaultPoolLocked(next)
	return f.poolOut(next), nil
}

// DeleteRelayPool refuses a pool a network uses or a tenant's access
// names (RELAY_POOL_IN_USE).
func (f *fakeGuardServer) DeleteRelayPool(ctx context.Context, in *guardpb.DeleteRelayPoolRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.pools[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "relay pool %q not found", in.GetName())
	}
	for _, n := range sortedValues(f.networks) {
		if n.GetRelayPool() == in.GetName() {
			return nil, guardRefusal(codes.FailedPrecondition, "RELAY_POOL_IN_USE",
				fmt.Sprintf("relay pool %s is in use by network %s", in.GetName(), n.GetName()))
		}
	}
	for tenant, a := range f.access {
		if a.GetDefaultPool() == in.GetName() || slices.Contains(a.GetAllowedPools(), in.GetName()) {
			return nil, guardRefusal(codes.FailedPrecondition, "RELAY_POOL_IN_USE",
				fmt.Sprintf("relay pool %s is in use by %s's relay pool access", in.GetName(), tenant))
		}
	}
	delete(f.pools, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- PoolRelay ---

func (f *fakeGuardServer) checkPoolRelayLocked(r *guardpb.PoolRelay) error {
	if _, ok := f.pools[r.GetPool()]; !ok {
		return status.Errorf(codes.InvalidArgument, "pool %q is not a relay pool", r.GetPool())
	}
	return checkRelay(&guardpb.Relay{Url: r.GetUrl()})
}

func (f *fakeGuardServer) CreatePoolRelay(ctx context.Context, in *guardpb.CreatePoolRelayRequest) (*guardpb.PoolRelay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r := proto.Clone(in.GetPoolRelay()).(*guardpb.PoolRelay)
	if err := f.checkPoolRelayLocked(r); err != nil {
		return nil, err
	}
	r.Name = poolRelaysPrefix + f.nextID("rl")
	f.poolRelays[r.GetName()] = r
	return proto.Clone(r).(*guardpb.PoolRelay), nil
}

func (f *fakeGuardServer) GetPoolRelay(ctx context.Context, in *guardpb.GetPoolRelayRequest) (*guardpb.PoolRelay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.poolRelays[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "pool relay %q not found", in.GetName())
	}
	return proto.Clone(r).(*guardpb.PoolRelay), nil
}

// ListPoolRelays ignores filter, as guard-control#81 does.
func (f *fakeGuardServer) ListPoolRelays(ctx context.Context, _ *guardpb.ListPoolRelaysRequest) (*guardpb.ListPoolRelaysResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListPoolRelaysResponse{}
	for _, r := range sortedValues(f.poolRelays) {
		out.PoolRelays = append(out.PoolRelays, proto.Clone(r).(*guardpb.PoolRelay))
	}
	return out, nil
}

func (f *fakeGuardServer) PatchPoolRelay(ctx context.Context, in *guardpb.PatchPoolRelayRequest) (*guardpb.PoolRelay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.poolRelays[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "pool relay %q not found", in.GetName())
	}
	next := proto.Clone(r).(*guardpb.PoolRelay)
	if err := applyMask(next, in.GetPoolRelay(), in.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if err := f.checkPoolRelayLocked(next); err != nil {
		return nil, err
	}
	f.poolRelays[in.GetName()] = next
	return proto.Clone(next).(*guardpb.PoolRelay), nil
}

// DeletePoolRelay revokes its credential with it.
func (f *fakeGuardServer) DeletePoolRelay(ctx context.Context, in *guardpb.DeletePoolRelayRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.poolRelays[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "pool relay %q not found", in.GetName())
	}
	delete(f.poolRelays, in.GetName())
	delete(f.creds, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Credentials ---

func (f *fakeGuardServer) issueLocked(name string) *guardpb.RelayCredential {
	c := "grk_" + f.nextID("cred")
	f.creds[name] = c
	return &guardpb.RelayCredential{Name: name, Credential: c, CreatedAt: timestamppb.Now()}
}

func (f *fakeGuardServer) IssuePoolRelayCredential(ctx context.Context, in *guardpb.IssuePoolRelayCredentialRequest) (*guardpb.RelayCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.poolRelays[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "pool relay %q not found", in.GetName())
	}
	r.HasCredential = true
	return f.issueLocked(in.GetName()), nil
}

func (f *fakeGuardServer) IssueRelayCredential(ctx context.Context, in *guardpb.IssueRelayCredentialRequest) (*guardpb.RelayCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.relays[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "relay %q not found", in.GetName())
	}
	r.HasCredential = true
	return f.issueLocked(in.GetName()), nil
}

// revoke is an operator's :revokeCredential, which terraform never calls.
func (f *fakeGuardServer) revoke(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.creds, name)
	if r, ok := f.poolRelays[name]; ok {
		r.HasCredential = false
	}
	if r, ok := f.relays[name]; ok {
		r.HasCredential = false
	}
}

// credential is what guard-control holds for a relay, "" when none.
func (f *fakeGuardServer) credential(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creds[name]
}

// --- Access ---

func (f *fakeGuardServer) ListAllowedRelayPools(ctx context.Context, in *guardpb.ListAllowedRelayPoolsRequest) (*guardpb.ListAllowedRelayPoolsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.tenants[in.GetParent()]; !ok {
		return nil, guardRefusal(codes.FailedPrecondition, "GUARD_NOT_ENABLED", "Guard is not enabled on tenant "+in.GetParent())
	}
	def := f.tenantDefaultPoolLocked(in.GetParent())
	out := &guardpb.ListAllowedRelayPoolsResponse{}
	seen := map[string]bool{}
	for _, name := range f.allowedPoolsLocked(in.GetParent()) {
		p, ok := f.pools[name]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		out.RelayPools = append(out.RelayPools, &guardpb.AllowedRelayPool{
			Name: p.GetName(), Slug: p.GetSlug(), DisplayName: p.GetDisplayName(), Region: p.GetRegion(), Default: name == def,
		})
	}
	slices.SortStableFunc(out.GetRelayPools(), func(a, b *guardpb.AllowedRelayPool) int {
		switch {
		case a.GetDefault() && !b.GetDefault():
			return -1
		case b.GetDefault() && !a.GetDefault():
			return 1
		}
		return strings.Compare(a.GetSlug(), b.GetSlug())
	})
	return out, nil
}

func accessTenant(name string) string {
	return strings.TrimSuffix(name, "/relay-pool-access")
}

func (f *fakeGuardServer) GetRelayPoolAccess(ctx context.Context, in *guardpb.GetRelayPoolAccessRequest) (*guardpb.RelayPoolAccess, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	tenant := accessTenant(in.GetName())
	if _, ok := f.tenants[tenant]; !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", tenant)
	}
	if a, ok := f.access[tenant]; ok {
		return proto.Clone(a).(*guardpb.RelayPoolAccess), nil
	}
	return &guardpb.RelayPoolAccess{Name: in.GetName()}, nil
}

func (f *fakeGuardServer) UpdateRelayPoolAccess(ctx context.Context, in *guardpb.UpdateRelayPoolAccessRequest) (*guardpb.RelayPoolAccess, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	a := proto.Clone(in.GetRelayPoolAccess()).(*guardpb.RelayPoolAccess)
	tenant := accessTenant(a.GetName())
	if _, ok := f.tenants[tenant]; !ok {
		return nil, guardRefusal(codes.FailedPrecondition, "GUARD_NOT_ENABLED", "Guard is not enabled on tenant "+tenant)
	}
	for _, name := range append(slices.Clone(a.GetAllowedPools()), a.GetDefaultPool()) {
		if _, ok := f.pools[name]; name != "" && !ok {
			return nil, status.Errorf(codes.InvalidArgument, "%q is not a relay pool", name)
		}
	}
	f.access[tenant] = a
	return proto.Clone(a).(*guardpb.RelayPoolAccess), nil
}
