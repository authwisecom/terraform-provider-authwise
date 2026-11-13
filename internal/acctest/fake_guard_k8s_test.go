package acctest_test

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Guard for Kubernetes (train 2026.11.GFK.1) as apis v0.73.0 states it:
// node groups (GuardNodeGroupService), cluster trusts and their rules
// (GuardClusterTrustService), enrollment keys (GuardEnrollmentKeyService,
// guard-control#98) and translated applications on Resource. guard-control's
// servers were not built when this was written (feat/gfk1 has the contract
// only), so the rules are the proto comments', not a server's.

const (
	nodeGroupsSeg     = "/nodeGroups/"
	clusterTrustsSeg  = "/clusterTrusts/"
	enrollmentKeysSeg = "/enrollmentKeys/"

	reasonNodeGroupInUse      = "NODE_GROUP_IN_USE"
	reasonNodeLacksCapability = "NODE_LACKS_CAPABILITY"
	capabilityAppTranslate    = "app-translate"

	defaultRuleMaxNodes = 100
	maxRuleMaxNodes     = 1000
	maxKeyUses          = 1000
	defaultKeyLife      = 30 * 24 * time.Hour
	maxKeyLife          = 90 * 24 * time.Hour
)

var (
	guardTrustParent = regexp.MustCompile(`^tenants/[^/]+/networks/[^/]+/clusterTrusts/[^/]+$`)
	dnsLabel32       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
)

// fakeGuardK8s is the GFK half of fakeGuardServer's state.
type fakeGuardK8s struct {
	groups map[string]*guardpb.NodeGroup
	// members are each group's node names.
	members map[string][]string
	trusts  map[string]*guardpb.ClusterTrust
	rules   map[string]*guardpb.ClusterTrustRule
	keys    map[string]*guardpb.EnrollmentKey
}

func newFakeGuardK8s() *fakeGuardK8s {
	return &fakeGuardK8s{
		groups:  map[string]*guardpb.NodeGroup{},
		members: map[string][]string{},
		trusts:  map[string]*guardpb.ClusterTrust{},
		rules:   map[string]*guardpb.ClusterTrustRule{},
		keys:    map[string]*guardpb.EnrollmentKey{},
	}
}

// groupsOfLocked is the sorted group names a node is a member of.
func (f *fakeGuardServer) groupsOfLocked(node string) []string {
	var out []string
	for g, ms := range f.k8s.members {
		if slices.Contains(ms, node) {
			out = append(out, g)
		}
	}
	slices.Sort(out)
	return out
}

// --- GuardNodeGroupService ---

func (f *fakeGuardServer) groupOut(g *guardpb.NodeGroup) *guardpb.NodeGroup {
	out := proto.Clone(g).(*guardpb.NodeGroup)
	out.MemberCount = int32(len(f.k8s.members[g.GetName()]))
	return out
}

func (f *fakeGuardServer) checkGroupNameLocked(network, name, displayName string) error {
	if displayName == "" || len(displayName) > 64 {
		return status.Error(codes.InvalidArgument, "display_name: required, at most 64 characters")
	}
	for _, g := range children(f.k8s.groups, network+nodeGroupsSeg) {
		if g.GetName() != name && g.GetDisplayName() == displayName {
			return status.Errorf(codes.AlreadyExists, "a node group named %q exists in the network", displayName)
		}
	}
	return nil
}

func (f *fakeGuardServer) CreateNodeGroup(ctx context.Context, in *guardpb.CreateNodeGroupRequest) (*guardpb.NodeGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, err := f.parentNetwork(in.GetParent()); err != nil {
		return nil, err
	}
	g := proto.Clone(in.GetNodeGroup()).(*guardpb.NodeGroup)
	if err := f.checkGroupNameLocked(in.GetParent(), "", g.GetDisplayName()); err != nil {
		return nil, err
	}
	g.Name = in.GetParent() + nodeGroupsSeg + f.nextID("ng")
	g.CreatedBy = "c-terraform"
	g.CreatedAt = timestamppb.Now()
	g.UpdatedAt = g.GetCreatedAt()
	f.k8s.groups[g.GetName()] = g
	return f.groupOut(g), nil
}

func (f *fakeGuardServer) GetNodeGroup(ctx context.Context, in *guardpb.GetNodeGroupRequest) (*guardpb.NodeGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	g, ok := f.k8s.groups[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "node group %q not found", in.GetName())
	}
	return f.groupOut(g), nil
}

func (f *fakeGuardServer) ListNodeGroups(ctx context.Context, in *guardpb.ListNodeGroupsRequest) (*guardpb.ListNodeGroupsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListNodeGroupsResponse{}
	for _, g := range children(f.k8s.groups, in.GetParent()+nodeGroupsSeg) {
		out.NodeGroups = append(out.NodeGroups, f.groupOut(g))
	}
	return out, nil
}

func (f *fakeGuardServer) UpdateNodeGroup(ctx context.Context, in *guardpb.UpdateNodeGroupRequest) (*guardpb.NodeGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	name := in.GetNodeGroup().GetName()
	g, ok := f.k8s.groups[name]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "node group %q not found", name)
	}
	next := proto.Clone(g).(*guardpb.NodeGroup)
	for _, p := range in.GetUpdateMask().GetPaths() {
		switch p {
		case "display_name":
			next.DisplayName = in.GetNodeGroup().GetDisplayName()
		case "description":
			next.Description = in.GetNodeGroup().GetDescription()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: unknown path %q", p)
		}
	}
	if err := f.checkGroupNameLocked(networkOf(name), name, next.GetDisplayName()); err != nil {
		return nil, err
	}
	next.UpdatedAt = timestamppb.Now()
	f.k8s.groups[name] = next
	return f.groupOut(next), nil
}

// DeleteNodeGroup is refused while an ACTIVE key or a rule names the
// group; otherwise memberships, grants and serving go with it.
func (f *fakeGuardServer) DeleteNodeGroup(ctx context.Context, in *guardpb.DeleteNodeGroupRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	name := in.GetName()
	if _, ok := f.k8s.groups[name]; !ok {
		return nil, status.Errorf(codes.NotFound, "node group %q not found", name)
	}
	for _, k := range f.k8s.keys {
		// Only an ACTIVE key holds its group (guard-control, GK5): an
		// expired, used-up or revoked one admits no join.
		if k.GetNodeGroup() == name && keyOut(k).GetState() == guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_ACTIVE {
			return nil, guardRefusal(codes.FailedPrecondition, reasonNodeGroupInUse, "enrollment key "+k.GetName()+" names the group")
		}
	}
	for _, r := range f.k8s.rules {
		if r.GetNodeGroup() == name {
			return nil, guardRefusal(codes.FailedPrecondition, reasonNodeGroupInUse, "cluster trust rule "+r.GetName()+" names the group")
		}
	}
	delete(f.k8s.groups, name)
	delete(f.k8s.members, name)
	delete(f.grants, name)
	for k, targets := range f.grants {
		f.grants[k] = slices.DeleteFunc(targets, func(s string) bool { return s == name })
	}
	for _, r := range f.resources {
		r.ServingNodeGroups = slices.DeleteFunc(r.GetServingNodeGroups(), func(s string) bool { return s == name })
	}
	return &emptypb.Empty{}, nil
}

func (f *fakeGuardServer) AssociateNodesToNodeGroup(ctx context.Context, in *guardpb.AssociateNodesToNodeGroupRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	name := in.GetName()
	if _, ok := f.k8s.groups[name]; !ok {
		return nil, status.Errorf(codes.NotFound, "node group %q not found", name)
	}
	network := networkOf(name)
	ms := slices.Clone(f.k8s.members[name])
	for _, n := range in.GetAssociation().GetSet() {
		if !strings.Contains(n, "/") {
			n = network + "/nodes/" + n
		}
		if _, ok := f.nodes[n]; !ok || networkOf(n) != network {
			return nil, status.Errorf(codes.InvalidArgument, "%s is not a node of %s", n, network)
		}
		if !slices.Contains(ms, n) {
			ms = append(ms, n)
		}
	}
	for _, n := range in.GetAssociation().GetRemove() {
		ms = slices.DeleteFunc(ms, func(s string) bool { return s == n })
	}
	slices.Sort(ms)
	f.k8s.members[name] = ms
	return &emptypb.Empty{}, nil
}

// grantKindLocked is a grant target's kind, or "" when it is none of a
// network's resources, nodes and node groups.
func (f *fakeGuardServer) grantKindLocked(target string) (kind, displayName string) {
	if r, ok := f.resources[target]; ok {
		return "resource", r.GetDisplayName()
	}
	if n, ok := f.nodes[target]; ok {
		return "node", n.GetDisplayName()
	}
	if g, ok := f.k8s.groups[target]; ok {
		return "nodeGroup", g.GetDisplayName()
	}
	return "", ""
}

func (f *fakeGuardServer) ListNodeGroupGrants(ctx context.Context, in *guardpb.ListNodeGroupGrantsRequest) (*guardpb.ListNodeGroupGrantsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.k8s.groups[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "node group %q not found", in.GetName())
	}
	out := &guardpb.ListNodeGroupGrantsResponse{}
	for _, t := range f.grants[in.GetName()] {
		kind, dn := f.grantKindLocked(t)
		out.Grants = append(out.Grants, &guardpb.NodeGrant{Target: t, Kind: kind, DisplayName: dn})
	}
	return out, nil
}

func (f *fakeGuardServer) AssociateGrantsToNodeGroup(ctx context.Context, in *guardpb.AssociateGrantsToNodeGroupRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.k8s.groups[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "node group %q not found", in.GetName())
	}
	network := networkOf(in.GetName())
	targets := slices.Clone(f.grants[in.GetName()])
	for _, t := range in.GetAssociation().GetSet() {
		if kind, _ := f.grantKindLocked(t); kind == "" || networkOf(t) != network {
			return nil, status.Errorf(codes.InvalidArgument, "%s is not a resource, node or node group of %s", t, network)
		}
		if !slices.Contains(targets, t) {
			targets = append(targets, t)
		}
	}
	for _, t := range in.GetAssociation().GetRemove() {
		targets = slices.DeleteFunc(targets, func(s string) bool { return s == t })
	}
	slices.Sort(targets)
	f.grants[in.GetName()] = targets
	return &emptypb.Empty{}, nil
}

// --- GuardClusterTrustService ---

func (f *fakeGuardServer) checkTrustKeysLocked(t *guardpb.ClusterTrust) error {
	switch k := t.GetKeys().(type) {
	case *guardpb.ClusterTrust_Discover:
		if !k.Discover {
			return status.Error(codes.InvalidArgument, "discover: true or absent")
		}
		if !strings.HasPrefix(t.GetIssuer(), "https://") {
			return status.Error(codes.InvalidArgument, "issuer: discover needs an https issuer")
		}
		t.KeyIds = []string{"kid-discovered"}
		t.KeysUpdatedAt = timestamppb.Now()
	case *guardpb.ClusterTrust_Jwks:
		if !strings.Contains(string(k.Jwks), `"keys"`) || len(k.Jwks) > 64<<10 {
			return status.Error(codes.InvalidArgument, "jwks: not a JWKS document of at most 64 KiB")
		}
		t.KeyIds = []string{fmt.Sprintf("kid-uploaded-%d", len(k.Jwks))}
		t.KeysUpdatedAt = timestamppb.Now()
	default:
		return status.Error(codes.InvalidArgument, "keys: give discover or jwks")
	}
	return nil
}

// trustOut never returns the uploaded jwks.
func trustOut(t *guardpb.ClusterTrust) *guardpb.ClusterTrust {
	out := proto.Clone(t).(*guardpb.ClusterTrust)
	if _, ok := out.GetKeys().(*guardpb.ClusterTrust_Jwks); ok {
		out.Keys = nil
	}
	return out
}

func (f *fakeGuardServer) CreateClusterTrust(ctx context.Context, in *guardpb.CreateClusterTrustRequest) (*guardpb.ClusterTrust, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, err := f.parentNetwork(in.GetParent()); err != nil {
		return nil, err
	}
	t := proto.Clone(in.GetClusterTrust()).(*guardpb.ClusterTrust)
	if t.GetDisplayName() == "" || t.GetIssuer() == "" {
		return nil, status.Error(codes.InvalidArgument, "display_name and issuer are required")
	}
	if err := f.checkTrustKeysLocked(t); err != nil {
		return nil, err
	}
	t.Name = in.GetParent() + clusterTrustsSeg + f.nextID("ct")
	t.Audience = "guard:" + in.GetParent()
	t.CreatedBy = "c-terraform"
	t.CreatedAt = timestamppb.Now()
	f.k8s.trusts[t.GetName()] = t
	return trustOut(t), nil
}

func (f *fakeGuardServer) GetClusterTrust(ctx context.Context, in *guardpb.GetClusterTrustRequest) (*guardpb.ClusterTrust, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	t, ok := f.k8s.trusts[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster trust %q not found", in.GetName())
	}
	return trustOut(t), nil
}

func (f *fakeGuardServer) ListClusterTrusts(ctx context.Context, in *guardpb.ListClusterTrustsRequest) (*guardpb.ListClusterTrustsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListClusterTrustsResponse{}
	for _, t := range children(f.k8s.trusts, in.GetParent()+clusterTrustsSeg) {
		out.ClusterTrusts = append(out.ClusterTrusts, trustOut(t))
	}
	return out, nil
}

func (f *fakeGuardServer) UpdateClusterTrust(ctx context.Context, in *guardpb.UpdateClusterTrustRequest) (*guardpb.ClusterTrust, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	src := in.GetClusterTrust()
	t, ok := f.k8s.trusts[src.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster trust %q not found", src.GetName())
	}
	next := proto.Clone(t).(*guardpb.ClusterTrust)
	keys := false
	for _, p := range in.GetUpdateMask().GetPaths() {
		switch p {
		case "display_name":
			next.DisplayName = src.GetDisplayName()
		case "description":
			next.Description = src.GetDescription()
		case "discover", "jwks":
			next.Keys, keys = src.GetKeys(), true
		default:
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: unknown path %q (issuer never changes)", p)
		}
	}
	if keys {
		if err := f.checkTrustKeysLocked(next); err != nil {
			return nil, err
		}
	}
	f.k8s.trusts[src.GetName()] = next
	return trustOut(next), nil
}

func (f *fakeGuardServer) RevokeClusterTrust(ctx context.Context, in *guardpb.RevokeClusterTrustRequest) (*guardpb.ClusterTrust, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	t, ok := f.k8s.trusts[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster trust %q not found", in.GetName())
	}
	if t.GetRevokedAt() == nil {
		t.RevokedAt = timestamppb.Now()
		t.RevokedBy = "c-terraform"
	}
	return trustOut(t), nil
}

func (f *fakeGuardServer) checkRuleLocked(network string, r *guardpb.ClusterTrustRule) error {
	if r.GetNodeNamePrefix() != "" && (!dnsLabel32.MatchString(r.GetNodeNamePrefix()) || r.GetNodeNamePrefix() == "nd") {
		return status.Errorf(codes.InvalidArgument, "node_name_prefix: %q is not a DNS label of at most 32 characters, or is nd", r.GetNodeNamePrefix())
	}
	if r.GetMaxNodes() < 1 || r.GetMaxNodes() > maxRuleMaxNodes {
		return status.Errorf(codes.InvalidArgument, "max_nodes: 1 to %d", maxRuleMaxNodes)
	}
	if g := r.GetNodeGroup(); g != "" {
		if _, ok := f.k8s.groups[g]; !ok || networkOf(g) != network {
			return status.Errorf(codes.InvalidArgument, "node_group: %s is not a node group of %s", g, network)
		}
	}
	return nil
}

func (f *fakeGuardServer) CreateClusterTrustRule(ctx context.Context, in *guardpb.CreateClusterTrustRuleRequest) (*guardpb.ClusterTrustRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if !guardTrustParent.MatchString(in.GetParent()) {
		return nil, status.Errorf(codes.InvalidArgument, "parent %q is not a cluster trust", in.GetParent())
	}
	if _, ok := f.k8s.trusts[in.GetParent()]; !ok {
		return nil, status.Errorf(codes.NotFound, "cluster trust %q not found", in.GetParent())
	}
	r := proto.Clone(in.GetRule()).(*guardpb.ClusterTrustRule)
	if r.GetNamespace() == "" || r.GetServiceAccount() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace and service_account are required")
	}
	for _, other := range children(f.k8s.rules, in.GetParent()+"/rules/") {
		if other.GetNamespace() == r.GetNamespace() && other.GetServiceAccount() == r.GetServiceAccount() {
			return nil, status.Errorf(codes.AlreadyExists, "a rule for %s/%s exists in the trust", r.GetNamespace(), r.GetServiceAccount())
		}
	}
	if r.GetMaxNodes() == 0 {
		r.MaxNodes = defaultRuleMaxNodes
	}
	if err := f.checkRuleLocked(networkOf(in.GetParent()), r); err != nil {
		return nil, err
	}
	r.Name = in.GetParent() + "/rules/" + f.nextID("ctr")
	r.CreatedBy = "c-terraform"
	r.CreatedAt = timestamppb.Now()
	f.k8s.rules[r.GetName()] = r
	return proto.Clone(r).(*guardpb.ClusterTrustRule), nil
}

func (f *fakeGuardServer) GetClusterTrustRule(ctx context.Context, in *guardpb.GetClusterTrustRuleRequest) (*guardpb.ClusterTrustRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	r, ok := f.k8s.rules[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "rule %q not found", in.GetName())
	}
	return proto.Clone(r).(*guardpb.ClusterTrustRule), nil
}

func (f *fakeGuardServer) ListClusterTrustRules(ctx context.Context, in *guardpb.ListClusterTrustRulesRequest) (*guardpb.ListClusterTrustRulesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListClusterTrustRulesResponse{}
	for _, r := range children(f.k8s.rules, in.GetParent()+"/rules/") {
		out.Rules = append(out.Rules, proto.Clone(r).(*guardpb.ClusterTrustRule))
	}
	return out, nil
}

func (f *fakeGuardServer) UpdateClusterTrustRule(ctx context.Context, in *guardpb.UpdateClusterTrustRuleRequest) (*guardpb.ClusterTrustRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	src := in.GetRule()
	r, ok := f.k8s.rules[src.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "rule %q not found", src.GetName())
	}
	next := proto.Clone(r).(*guardpb.ClusterTrustRule)
	for _, p := range in.GetUpdateMask().GetPaths() {
		switch p {
		case "node_name_prefix":
			next.NodeNamePrefix = src.GetNodeNamePrefix()
		case "ephemeral":
			next.Ephemeral = src.GetEphemeral()
		case "node_group":
			next.NodeGroup = src.GetNodeGroup()
		case "max_nodes":
			next.MaxNodes = src.GetMaxNodes()
		case "description":
			next.Description = src.GetDescription()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: unknown path %q (namespace and service_account never change)", p)
		}
	}
	if err := f.checkRuleLocked(networkOf(src.GetName()), next); err != nil {
		return nil, err
	}
	f.k8s.rules[src.GetName()] = next
	return proto.Clone(next).(*guardpb.ClusterTrustRule), nil
}

func (f *fakeGuardServer) DeleteClusterTrustRule(ctx context.Context, in *guardpb.DeleteClusterTrustRuleRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.k8s.rules[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "rule %q not found", in.GetName())
	}
	delete(f.k8s.rules, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- GuardEnrollmentKeyService ---

func keyOut(k *guardpb.EnrollmentKey) *guardpb.EnrollmentKey {
	out := proto.Clone(k).(*guardpb.EnrollmentKey)
	out.Secret = ""
	out.Ttl = nil
	switch {
	case out.GetRevokedAt() != nil:
		out.State = guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_REVOKED
	case out.GetExpiresAt().AsTime().Before(time.Now()):
		out.State = guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_EXPIRED
	case out.GetUses() >= out.GetMaxUses():
		out.State = guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_USED_UP
	default:
		out.State = guardpb.EnrollmentKeyState_ENROLLMENT_KEY_STATE_ACTIVE
	}
	return out
}

func (f *fakeGuardServer) checkKeyLocked(network string, k *guardpb.EnrollmentKey) error {
	if k.GetMaxUses() == 0 {
		k.MaxUses = 1
	}
	if k.GetMaxUses() < 1 || k.GetMaxUses() > maxKeyUses {
		return status.Errorf(codes.InvalidArgument, "max_uses: 1 to %d", maxKeyUses)
	}
	if k.GetEphemeral() && k.GetMaxUses() == 1 {
		return status.Error(codes.InvalidArgument, "ephemeral: refused on a one-use key")
	}
	if k.GetNodeNamePrefix() != "" && (!dnsLabel32.MatchString(k.GetNodeNamePrefix()) || k.GetNodeNamePrefix() == "nd") {
		return status.Errorf(codes.InvalidArgument, "node_name_prefix: %q is not a DNS label of at most 32 characters, or is nd", k.GetNodeNamePrefix())
	}
	if g := k.GetNodeGroup(); g != "" {
		if _, ok := f.k8s.groups[g]; !ok || networkOf(g) != network {
			return status.Errorf(codes.InvalidArgument, "node_group: %s is not a node group of %s", g, network)
		}
	}
	life := defaultKeyLife
	switch {
	case k.GetTtl() != nil && k.GetExpiresAt() != nil:
		return status.Error(codes.InvalidArgument, "give at most one of expires_at and ttl")
	case k.GetTtl() != nil:
		life = k.GetTtl().AsDuration()
	case k.GetExpiresAt() != nil:
		life = time.Until(k.GetExpiresAt().AsTime())
	}
	if life < 24*time.Hour || life > maxKeyLife {
		return status.Error(codes.InvalidArgument, "the key's life is 1 to 90 days")
	}
	k.ExpiresAt = timestamppb.New(time.Now().Add(life))
	return nil
}

func (f *fakeGuardServer) CreateEnrollmentKey(ctx context.Context, in *guardpb.CreateEnrollmentKeyRequest) (*guardpb.EnrollmentKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, err := f.parentNetwork(in.GetParent()); err != nil {
		return nil, err
	}
	k := proto.Clone(in.GetEnrollmentKey()).(*guardpb.EnrollmentKey)
	if err := f.checkKeyLocked(in.GetParent(), k); err != nil {
		return nil, err
	}
	k.Name = in.GetParent() + enrollmentKeysSeg + f.nextID("ek")
	k.CreatedBy = "c-terraform"
	k.CreatedAt = timestamppb.Now()
	k.Ttl = nil
	f.k8s.keys[k.GetName()] = k
	out := keyOut(k)
	out.Secret = "gk_" + k.GetName()[strings.LastIndex(k.GetName(), "/")+1:]
	return out, nil
}

func (f *fakeGuardServer) GetEnrollmentKey(ctx context.Context, in *guardpb.GetEnrollmentKeyRequest) (*guardpb.EnrollmentKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	k, ok := f.k8s.keys[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "enrollment key %q not found", in.GetName())
	}
	return keyOut(k), nil
}

func (f *fakeGuardServer) ListEnrollmentKeys(ctx context.Context, in *guardpb.ListEnrollmentKeysRequest) (*guardpb.ListEnrollmentKeysResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListEnrollmentKeysResponse{}
	for _, k := range children(f.k8s.keys, in.GetParent()+enrollmentKeysSeg) {
		out.EnrollmentKeys = append(out.EnrollmentKeys, keyOut(k))
	}
	return out, nil
}

func (f *fakeGuardServer) RevokeEnrollmentKey(ctx context.Context, in *guardpb.RevokeEnrollmentKeyRequest) (*guardpb.EnrollmentKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	k, ok := f.k8s.keys[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "enrollment key %q not found", in.GetName())
	}
	if k.GetRevokedAt() == nil {
		k.RevokedAt = timestamppb.Now()
		k.RevokedBy = "c-terraform"
	}
	return keyOut(k), nil
}

// joinWithKey is a machine joining with a key, as the agent's enrollment
// would: a server node, the key's use counted, and the key's group joined.
func (f *fakeGuardServer) joinWithKey(key, host string) string {
	f.mu.Lock()
	k := f.k8s.keys[key]
	network := networkOf(key)
	f.mu.Unlock()
	name := f.seedNode(network, host)
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[name]
	n.OwnerId = ""
	n.EnrollmentKey = key
	n.Ephemeral = k.GetEphemeral()
	k.Uses++
	k.LastUsedAt = timestamppb.Now()
	if g := k.GetNodeGroup(); g != "" {
		f.k8s.members[g] = append(f.k8s.members[g], name)
	}
	return name
}

// dropNodes deletes nodes as an administrator would, with their
// memberships.
func (f *fakeGuardServer) dropNodes(names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		delete(f.nodes, n)
		for g, ms := range f.k8s.members {
			f.k8s.members[g] = slices.DeleteFunc(ms, func(s string) bool { return s == n })
		}
	}
}

// expireKey moves a key's expiry into the past, as time would.
func (f *fakeGuardServer) expireKey(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.k8s.keys[key].ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
}

// revokeTrust revokes a trust out of band, as the console would.
func (f *fakeGuardServer) revokeTrust(trust string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.k8s.trusts[trust].RevokedAt = timestamppb.Now()
}

// --- Resource: translated applications and serving groups ---

// translateOnCreateLocked applies the translation defaults to a resource
// being created: translated when a serving node at create runs on
// Kubernetes, unless the request says; a translated application gets a
// virtual address and a published name.
func (f *fakeGuardServer) translateOnCreateLocked(network string, r *guardpb.Resource) error {
	if r.Translated == nil {
		k8s := false
		for _, id := range r.GetServingNodeIds() {
			if f.nodes[network+"/nodes/"+id].GetPlatform() == guardpb.Platform_PLATFORM_KUBERNETES {
				k8s = true
			}
		}
		for _, g := range r.GetServingNodeGroups() {
			for _, m := range f.k8s.members[g] {
				if f.nodes[m].GetPlatform() == guardpb.Platform_PLATFORM_KUBERNETES {
					k8s = true
				}
			}
		}
		r.Translated = proto.Bool(k8s)
	}
	if !r.GetTranslated() {
		if r.GetPublishedName() != "" {
			return status.Error(codes.InvalidArgument, "published_name: only on a translated application")
		}
		return nil
	}
	if r.GetKind() != "application" {
		return status.Error(codes.InvalidArgument, "translated: applications only")
	}
	if r.GetPublishedName() == "" {
		labels := strings.SplitN(r.GetAddress(), ".", 3)
		mesh := f.networks[network].GetMeshDomain()
		if mesh == "" {
			mesh = "office.acme.guard.example"
		}
		r.PublishedName = strings.Join(labels[:min(2, len(labels))], ".") + "." + mesh
	}
	address, err := f.allocateLocked(network)
	if err != nil {
		return err
	}
	r.VirtualAddress = address
	return nil
}

// checkTranslatorsLocked refuses a node without app-translate as a server
// of a translated resource.
func (f *fakeGuardServer) checkTranslatorsLocked(r *guardpb.Resource, nodes []string) error {
	if !r.GetTranslated() {
		return nil
	}
	for _, n := range nodes {
		if !slices.Contains(f.nodes[n].GetCapabilities(), capabilityAppTranslate) {
			return guardRefusal(codes.FailedPrecondition, reasonNodeLacksCapability, n+" has not reported "+capabilityAppTranslate)
		}
	}
	return nil
}

// associateGroupsToResourceLocked takes the node group names among an
// :associateNodes request's entries.
func (f *fakeGuardServer) associateGroupsToResourceLocked(r *guardpb.Resource, set, remove []string) error {
	groups := slices.Clone(r.GetServingNodeGroups())
	for _, g := range set {
		if _, ok := f.k8s.groups[g]; !ok || networkOf(g) != networkOf(r.GetName()) {
			return status.Errorf(codes.InvalidArgument, "%s is not a node group of %s", g, networkOf(r.GetName()))
		}
		if err := f.checkTranslatorsLocked(r, f.k8s.members[g]); err != nil {
			return err
		}
		if !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	for _, g := range remove {
		groups = slices.DeleteFunc(groups, func(s string) bool { return s == g })
	}
	slices.Sort(groups)
	r.ServingNodeGroups = groups
	return nil
}

func isNodeGroupName(name string) bool {
	return strings.Contains(name, nodeGroupsSeg)
}
