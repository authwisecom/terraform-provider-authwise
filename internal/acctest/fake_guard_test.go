package acctest_test

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeGuardServer is guard-control v0.15.0's admin API (apis v0.28.0) as the
// Guard resources use it: since the tenant collapse (apis v0.57.0, tf#65),
// Guard's settings for the kit tenant t-1, which kit reports Guard on, networks
// with a cidr in the mesh range, relays, resources and the nodes serving
// them. The rules are guard-control's creator.go and validations.go. It
// serves no invites: Guard's Invite entity is retired (tf#67).
// People's nodes are seeded by tests, as enrolment would make them; a
// server's node is created through CreateNode.
type fakeGuardServer struct {
	guardpb.UnimplementedGuardControlServiceServer
	guardpb.UnimplementedGuardNodeServiceServer
	guardpb.UnimplementedGuardRelayPoolServiceServer

	mu        sync.Mutex
	tenants   map[string]*guardpb.Tenant
	networks  map[string]*guardpb.Network
	relays    map[string]*guardpb.Relay
	resources map[string]*guardpb.Resource
	nodes     map[string]*guardpb.Node
	// Estate relay pools and their relays (tf#66), each tenant's access,
	// and the relays' credentials, kept apart as guard-control keeps only
	// a hash.
	pools      map[string]*guardpb.RelayPool
	poolRelays map[string]*guardpb.PoolRelay
	access     map[string]*guardpb.RelayPoolAccess
	creds      map[string]string
	// grants are each node's, by node name: resource and node names.
	grants map[string][]string
	seq    int
	// auths records the authorization header of every call, to show the
	// provider's bearer reaches guard-control too.
	auths []string
}

func newFakeGuardServer() *fakeGuardServer {
	f := &fakeGuardServer{
		tenants:    map[string]*guardpb.Tenant{},
		networks:   map[string]*guardpb.Network{},
		relays:     map[string]*guardpb.Relay{},
		resources:  map[string]*guardpb.Resource{},
		nodes:      map[string]*guardpb.Node{},
		pools:      map[string]*guardpb.RelayPool{},
		poolRelays: map[string]*guardpb.PoolRelay{},
		access:     map[string]*guardpb.RelayPoolAccess{},
		creds:      map[string]string{},
		grants:     map[string][]string{},
	}
	f.seedTenant("t-1")
	return f
}

// seedTenant adds Guard's settings for a kit tenant, as guard-control
// holds them once kit reports Guard on it: what kit owns is cached and
// output-only. A tenant not seeded has no Guard.
func (f *fakeGuardServer) seedTenant(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := "tenants/" + id
	f.tenants[name] = &guardpb.Tenant{
		Name:            name,
		Slug:            "acme",
		DisplayName:     "Acme",
		Status:          "ACTIVE",
		Issuer:          "https://id.example.com/" + id,
		Audience:        "a-guard",
		ConsoleClientId: "c-console",
		// The plan guard-control reads from kit's tenant (guard-control#88).
		Plan: &guardpb.TenantPlan{Plan: "TEAM", MembersLimit: 25, DevicesLimit: 100, RelayBytesLimit: 1 << 40},
	}
}

var (
	meshRange      = netip.MustParsePrefix("100.64.0.0/10")
	guardNetParent = regexp.MustCompile(`^tenants/[^/]+/networks/[^/]+$`)
	hostname       = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

func (f *fakeGuardServer) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%08d", prefix, f.seq)
}

func (f *fakeGuardServer) record(ctx context.Context) {
	f.auths = append(f.auths, authorizationOf(ctx))
}

// seedNode adds a node to a network, as enrolment would.
func (f *fakeGuardServer) seedNode(network, id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := network + "/nodes/" + id
	address, err := f.allocateLocked(network)
	if err != nil {
		panic(err)
	}
	f.nodes[name] = &guardpb.Node{Name: name, DisplayName: id, State: "ACTIVE", Address: address, OwnerId: "u-" + id}
	return name
}

// applyMask copies the masked top-level fields from src onto dst.
func applyMask(dst, src proto.Message, paths []string) error {
	d, s := dst.ProtoReflect(), src.ProtoReflect()
	for _, p := range paths {
		fd := d.Descriptor().Fields().ByName(protoreflect.Name(p))
		if fd == nil || p == "name" {
			return status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", p)
		}
		if s.Has(fd) {
			d.Set(fd, s.Get(fd))
		} else {
			d.Clear(fd)
		}
	}
	return nil
}

func (f *fakeGuardServer) parentNetwork(parent string) (*guardpb.Network, error) {
	if !guardNetParent.MatchString(parent) {
		return nil, status.Errorf(codes.InvalidArgument, "parent %q is not a network", parent)
	}
	n, ok := f.networks[parent]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "network %q not found", parent)
	}
	return n, nil
}

func (f *fakeGuardServer) hasNodes(network string) bool {
	for name := range f.nodes {
		if strings.HasPrefix(name, network+"/nodes/") {
			return true
		}
	}
	return false
}

// --- Tenant ---

func (f *fakeGuardServer) GetTenant(ctx context.Context, in *guardpb.GetTenantRequest) (*guardpb.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	t, ok := f.tenants[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", in.GetName())
	}
	return proto.Clone(t).(*guardpb.Tenant), nil
}

func (f *fakeGuardServer) ListTenants(ctx context.Context, _ *guardpb.ListTenantsRequest) (*guardpb.ListTenantsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListTenantsResponse{}
	for _, t := range sortedValues(f.tenants) {
		out.Tenants = append(out.Tenants, proto.Clone(t).(*guardpb.Tenant))
	}
	return out, nil
}

// PatchTenant is an upsert of Guard's settings once kit reports Guard on
// the tenant; it refuses a tenant without Guard, or a suspended one, with
// guard-control's ErrorInfo reasons. Only the three writable paths are
// taken; the slug is kit's, refused with SLUG_MANAGED_BY_IDENTITY.
func (f *fakeGuardServer) PatchTenant(ctx context.Context, in *guardpb.PatchTenantRequest) (*guardpb.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	t, ok := f.tenants[in.GetTenant().GetName()]
	if !ok {
		return nil, guardRefusal(codes.FailedPrecondition, "GUARD_NOT_ENABLED", "Guard is not enabled on tenant "+in.GetTenant().GetName())
	}
	if t.GetStatus() == "SUSPENDED" {
		return nil, guardRefusal(codes.FailedPrecondition, "TENANT_SUSPENDED", "tenant "+t.GetName()+" is suspended")
	}
	next := proto.Clone(t).(*guardpb.Tenant)
	if next.GetConfig() == nil {
		next.Config = &guardpb.TenantConfig{}
	}
	src := in.GetTenant()
	for _, p := range in.GetUpdateMask().GetPaths() {
		switch p {
		case "slug":
			return nil, guardRefusal(codes.InvalidArgument, "SLUG_MANAGED_BY_IDENTITY", "the slug of "+t.GetName()+" is kit's tenant slug; set it on the tenant in kit")
		case "labels":
			next.Labels = src.GetLabels()
		case "config.users.realms":
			next.Config.Users = &guardpb.TenantUsersConfig{Realms: src.GetConfig().GetUsers().GetRealms()}
		case "config.deprovisioning.enabled":
			next.Config.Deprovisioning = &guardpb.TenantDeprovisioningConfig{Enabled: src.GetConfig().GetDeprovisioning().GetEnabled()}
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", p)
		}
	}
	f.tenants[t.GetName()] = next
	return proto.Clone(next).(*guardpb.Tenant), nil
}

// guardRefusal is a refusal carrying guard-control's ErrorInfo.
func guardRefusal(code codes.Code, reason, msg string) error {
	st, err := status.New(code, msg).WithDetails(&errdetails.ErrorInfo{Domain: "guard.authwise.com", Reason: reason})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

// --- Network ---

func checkNetworkCIDR(cidr string) error {
	if cidr == "" {
		return nil
	}
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() || p.Bits() < meshRange.Bits() || !meshRange.Contains(p.Addr()) || p.Masked() != p {
		return status.Errorf(codes.InvalidArgument, "cidr %q is not an IPv4 prefix inside %s", cidr, meshRange)
	}
	return nil
}

func (f *fakeGuardServer) CreateNetwork(ctx context.Context, in *guardpb.CreateNetworkRequest) (*guardpb.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.tenants[in.GetParent()]; !ok {
		return nil, guardRefusal(codes.FailedPrecondition, "GUARD_NOT_ENABLED", "Guard is not enabled on tenant "+in.GetParent())
	}
	n := proto.Clone(in.GetNetwork()).(*guardpb.Network)
	if err := checkNetworkCIDR(n.GetCidr()); err != nil {
		return nil, err
	}
	if n.GetCidr() == "" {
		n.Cidr = meshRange.String()
	}
	if err := f.checkNetworkPoolLocked(in.GetParent(), "", n, true); err != nil {
		return nil, err
	}
	n.Name = in.GetParent() + "/networks/" + f.nextID("n")
	f.networks[n.GetName()] = n
	return proto.Clone(n).(*guardpb.Network), nil
}

func (f *fakeGuardServer) GetNetwork(ctx context.Context, in *guardpb.GetNetworkRequest) (*guardpb.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	n, ok := f.networks[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "network %q not found", in.GetName())
	}
	return proto.Clone(n).(*guardpb.Network), nil
}

func (f *fakeGuardServer) ListNetworks(ctx context.Context, in *guardpb.ListNetworksRequest) (*guardpb.ListNetworksResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListNetworksResponse{}
	for _, n := range children(f.networks, in.GetParent()+"/networks/") {
		out.Networks = append(out.Networks, proto.Clone(n).(*guardpb.Network))
	}
	return out, nil
}

func (f *fakeGuardServer) PatchNetwork(ctx context.Context, in *guardpb.PatchNetworkRequest) (*guardpb.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	n, ok := f.networks[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "network %q not found", in.GetName())
	}
	next := proto.Clone(n).(*guardpb.Network)
	if err := applyMask(next, in.GetNetwork(), in.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if next.GetCidr() != n.GetCidr() {
		if f.hasNodes(in.GetName()) {
			return nil, status.Error(codes.FailedPrecondition, "cidr cannot change once the network has a node")
		}
		if err := checkNetworkCIDR(next.GetCidr()); err != nil {
			return nil, err
		}
	}
	tenant, _, _ := strings.Cut(in.GetName(), "/networks/")
	if err := f.checkNetworkPoolLocked(tenant, in.GetName(), next, false); err != nil {
		return nil, err
	}
	f.networks[in.GetName()] = next
	return proto.Clone(next).(*guardpb.Network), nil
}

func (f *fakeGuardServer) DeleteNetwork(ctx context.Context, in *guardpb.DeleteNetworkRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.networks[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "network %q not found", in.GetName())
	}
	prefix := in.GetName() + "/"
	for _, names := range [][]string{keys(f.relays), keys(f.resources), keys(f.nodes)} {
		for _, name := range names {
			if strings.HasPrefix(name, prefix) {
				return nil, status.Errorf(codes.FailedPrecondition, "network %s is not empty: %s remains", in.GetName(), name)
			}
		}
	}
	delete(f.networks, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Relay ---

func checkRelay(r *guardpb.Relay) error {
	if !strings.HasPrefix(r.GetUrl(), "wss://") && !strings.HasPrefix(r.GetUrl(), "https://") {
		return status.Errorf(codes.InvalidArgument, "url %q must be wss:// or https://", r.GetUrl())
	}
	return nil
}

func (f *fakeGuardServer) CreateRelay(ctx context.Context, in *guardpb.CreateRelayRequest) (*guardpb.Relay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, err := f.parentNetwork(in.GetParent()); err != nil {
		return nil, err
	}
	r := proto.Clone(in.GetRelay()).(*guardpb.Relay)
	if err := checkRelay(r); err != nil {
		return nil, err
	}
	r.Name = in.GetParent() + "/relays/" + f.nextID("rl")
	f.relays[r.GetName()] = r
	return proto.Clone(r).(*guardpb.Relay), nil
}

func (f *fakeGuardServer) GetRelay(ctx context.Context, in *guardpb.GetRelayRequest) (*guardpb.Relay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.relays[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "relay %q not found", in.GetName())
	}
	return proto.Clone(r).(*guardpb.Relay), nil
}

func (f *fakeGuardServer) ListRelays(ctx context.Context, in *guardpb.ListRelaysRequest) (*guardpb.ListRelaysResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListRelaysResponse{}
	for _, r := range children(f.relays, in.GetParent()+"/relays/") {
		out.Relays = append(out.Relays, proto.Clone(r).(*guardpb.Relay))
	}
	return out, nil
}

func (f *fakeGuardServer) PatchRelay(ctx context.Context, in *guardpb.PatchRelayRequest) (*guardpb.Relay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.relays[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "relay %q not found", in.GetName())
	}
	next := proto.Clone(r).(*guardpb.Relay)
	if err := applyMask(next, in.GetRelay(), in.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if err := checkRelay(next); err != nil {
		return nil, err
	}
	f.relays[in.GetName()] = next
	return proto.Clone(next).(*guardpb.Relay), nil
}

func (f *fakeGuardServer) DeleteRelay(ctx context.Context, in *guardpb.DeleteRelayRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.relays[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "relay %q not found", in.GetName())
	}
	delete(f.relays, in.GetName())
	delete(f.creds, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Resource ---

// checkResource is guard-control's Resource.Validate: a known kind, and an
// address of its shape outside the mesh range.
func checkResource(r *guardpb.Resource) error {
	switch r.GetKind() {
	case "subnet":
		p, err := netip.ParsePrefix(r.GetAddress())
		if err != nil || meshRange.Overlaps(p) {
			return status.Errorf(codes.InvalidArgument, "address %q is not a subnet outside %s", r.GetAddress(), meshRange)
		}
	case "host":
		a, err := netip.ParseAddr(r.GetAddress())
		if err != nil || meshRange.Contains(a) {
			return status.Errorf(codes.InvalidArgument, "address %q is not a routed address outside %s", r.GetAddress(), meshRange)
		}
	case "application":
		if !hostname.MatchString(r.GetAddress()) {
			return status.Errorf(codes.InvalidArgument, "address %q is not a DNS name", r.GetAddress())
		}
	default:
		return status.Errorf(codes.InvalidArgument,
			"kind: must be subnet, host or application (kind ip is gone: use host for a routed address, and grant a node directly to reach it), got %q", r.GetKind())
	}
	return nil
}

func (f *fakeGuardServer) CreateResource(ctx context.Context, in *guardpb.CreateResourceRequest) (*guardpb.Resource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, err := f.parentNetwork(in.GetParent()); err != nil {
		return nil, err
	}
	r := proto.Clone(in.GetResource()).(*guardpb.Resource)
	if err := checkResource(r); err != nil {
		return nil, err
	}
	for _, id := range r.GetServingNodeIds() {
		if _, ok := f.nodes[in.GetParent()+"/nodes/"+id]; !ok {
			return nil, status.Errorf(codes.InvalidArgument, "serving_node_ids: no node %s in the network", id)
		}
	}
	r.Name = in.GetParent() + "/resources/" + f.nextID("res")
	f.resources[r.GetName()] = r
	return proto.Clone(r).(*guardpb.Resource), nil
}

func (f *fakeGuardServer) GetResource(ctx context.Context, in *guardpb.GetResourceRequest) (*guardpb.Resource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.resources[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "resource %q not found", in.GetName())
	}
	return proto.Clone(r).(*guardpb.Resource), nil
}

func (f *fakeGuardServer) ListResources(ctx context.Context, in *guardpb.ListResourcesRequest) (*guardpb.ListResourcesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListResourcesResponse{}
	for _, r := range children(f.resources, in.GetParent()+"/resources/") {
		out.Resources = append(out.Resources, proto.Clone(r).(*guardpb.Resource))
	}
	return out, nil
}

func (f *fakeGuardServer) PatchResource(ctx context.Context, in *guardpb.PatchResourceRequest) (*guardpb.Resource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.resources[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "resource %q not found", in.GetName())
	}
	next := proto.Clone(r).(*guardpb.Resource)
	if err := applyMask(next, in.GetResource(), in.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if !slices.Equal(next.GetServingNodeIds(), r.GetServingNodeIds()) {
		return nil, status.Error(codes.InvalidArgument, "serving_node_ids may only echo the current set; :associateNodes changes it")
	}
	if err := checkResource(next); err != nil {
		return nil, err
	}
	f.resources[in.GetName()] = next
	return proto.Clone(next).(*guardpb.Resource), nil
}

func (f *fakeGuardServer) DeleteResource(ctx context.Context, in *guardpb.DeleteResourceRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.resources[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "resource %q not found", in.GetName())
	}
	delete(f.resources, in.GetName())
	return &emptypb.Empty{}, nil
}

func (f *fakeGuardServer) ListNodesByResource(ctx context.Context, in *guardpb.ListNodesByResourceRequest) (*guardpb.ListNodesByResourceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.resources[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "resource %q not found", in.GetName())
	}
	network := in.GetName()[:strings.LastIndex(in.GetName(), "/resources/")]
	out := &guardpb.ListNodesByResourceResponse{}
	for _, id := range r.GetServingNodeIds() {
		out.Nodes = append(out.Nodes, proto.Clone(f.nodes[network+"/nodes/"+id]).(*guardpb.Node))
	}
	return out, nil
}

func (f *fakeGuardServer) AssociateNodesToResource(ctx context.Context, in *guardpb.AssociateNodesToResourceRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.resources[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "resource %q not found", in.GetName())
	}
	network := in.GetName()[:strings.LastIndex(in.GetName(), "/resources/")]
	ids := slices.Clone(r.GetServingNodeIds())
	for _, name := range in.GetAssociation().GetSet() {
		if !strings.HasPrefix(name, network+"/nodes/") {
			return nil, status.Errorf(codes.InvalidArgument, "%s is not a node of %s", name, network)
		}
		if _, ok := f.nodes[name]; !ok {
			return nil, status.Errorf(codes.NotFound, "node %q not found", name)
		}
		if id := name[strings.LastIndex(name, "/")+1:]; !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, name := range in.GetAssociation().GetRemove() {
		id := name[strings.LastIndex(name, "/")+1:]
		ids = slices.DeleteFunc(ids, func(s string) bool { return s == id })
	}
	slices.Sort(ids)
	r.ServingNodeIds = ids
	return &emptypb.Empty{}, nil
}

// --- helpers ---

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sortedValues[V any](m map[string]V) []V {
	out := make([]V, 0, len(m))
	for _, k := range keys(m) {
		out = append(out, m[k])
	}
	return out
}

// children are the entries directly under prefix, in name order.
func children[V any](m map[string]V, prefix string) []V {
	var out []V
	for _, k := range keys(m) {
		if strings.HasPrefix(k, prefix) && !strings.Contains(k[len(prefix):], "/") {
			out = append(out, m[k])
		}
	}
	return out
}
