package acctest_test

import (
	"context"
	"fmt"
	"net/netip"
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

// Nodes and their grants (#32 stage 2), as guard-control v0.8.1 has them:
// creator.go's nodeCreator, addressing_node.go's Update and delete refusals,
// and GuardNodeService's grants. A node created without a public key is
// PENDING with a ga_ auth code returned once; with one it is ACTIVE.

const nodeAuthCodeTTL = 24 * time.Hour

// networkOf is the network name a node, resource or invite name sits under.
func networkOf(name string) string {
	parts := strings.Split(name, "/")
	return strings.Join(parts[:4], "/")
}

// allocateLocked is the network's next free address.
func (f *fakeGuardServer) allocateLocked(network string) (string, error) {
	p := netip.MustParsePrefix(f.networks[network].GetCidr())
	taken := map[string]bool{}
	for name, n := range f.nodes {
		if networkOf(name) == network {
			taken[n.GetAddress()] = true
		}
	}
	for a := p.Addr().Next(); p.Contains(a); a = a.Next() {
		if !taken[a.String()] {
			return a.String(), nil
		}
	}
	return "", status.Errorf(codes.ResourceExhausted, "network %s has no free address", network)
}

// deriveLocked fills a node's derived fields: allowed_ips is its own /32
// and the routed addresses it serves; roles is server when it serves a
// resource or another node is granted it.
func (f *fakeGuardServer) deriveLocked(n *guardpb.Node) {
	id := n.GetName()[strings.LastIndex(n.GetName(), "/")+1:]
	n.AllowedIps = []string{n.GetAddress() + "/32"}
	n.Roles = nil
	server := false
	for _, r := range f.resources {
		if networkOf(r.GetName()) == networkOf(n.GetName()) && slices.Contains(r.GetServingNodeIds(), id) {
			server = true
			if r.GetKind() != "application" {
				n.AllowedIps = append(n.AllowedIps, r.GetAddress())
			}
		}
	}
	for _, targets := range f.grants {
		if slices.Contains(targets, n.GetName()) {
			server = true
		}
	}
	if n.GetOwnerId() != "" && len(f.grants[n.GetName()]) > 0 {
		n.Roles = append(n.Roles, "client")
	}
	if server {
		n.Roles = append(n.Roles, "server")
	}
}

func (f *fakeGuardServer) readNodeLocked(name string) (*guardpb.Node, error) {
	n, ok := f.nodes[name]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "node %q not found", name)
	}
	out := proto.Clone(n).(*guardpb.Node)
	f.deriveLocked(out)
	out.AuthCode = ""
	return out, nil
}

func (f *fakeGuardServer) CreateNode(ctx context.Context, in *guardpb.CreateNodeRequest) (*guardpb.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, err := f.parentNetwork(in.GetParent()); err != nil {
		return nil, err
	}
	n := proto.Clone(in.GetNode()).(*guardpb.Node)
	switch {
	case n.GetState() != "":
		return nil, status.Error(codes.InvalidArgument, "state is output-only")
	case n.GetAuthCode() != "":
		return nil, status.Error(codes.InvalidArgument, "auth_code is output-only")
	case len(n.GetRoles()) > 0, len(n.GetAllowedIps()) > 0:
		return nil, status.Error(codes.InvalidArgument, "roles and allowed_ips are derived")
	}
	if n.GetAddress() == "" {
		a, err := f.allocateLocked(in.GetParent())
		if err != nil {
			return nil, err
		}
		n.Address = a
	} else if a, err := netip.ParseAddr(n.GetAddress()); err != nil ||
		!netip.MustParsePrefix(f.networks[in.GetParent()].GetCidr()).Contains(a) {
		return nil, status.Errorf(codes.InvalidArgument, "address %q is not inside the network's cidr", n.GetAddress())
	}
	n.Name = in.GetParent() + "/nodes/" + f.nextID("nd")

	code := ""
	if strings.TrimSpace(n.GetPublicKey()) == "" {
		n.PublicKey, n.State = "", "PENDING"
		n.AuthCodeExpiresAt = timestamppb.New(time.Now().UTC().Add(nodeAuthCodeTTL).Truncate(time.Second))
		f.seq++
		code = fmt.Sprintf("ga_code%d", f.seq)
	} else {
		n.State = "ACTIVE"
	}
	f.nodes[n.GetName()] = n

	out, err := f.readNodeLocked(n.GetName())
	if err != nil {
		return nil, err
	}
	out.AuthCode = code
	return out, nil
}

func (f *fakeGuardServer) GetNode(ctx context.Context, in *guardpb.GetNodeRequest) (*guardpb.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	return f.readNodeLocked(in.GetName())
}

func (f *fakeGuardServer) ListNodes(ctx context.Context, in *guardpb.ListNodesRequest) (*guardpb.ListNodesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	out := &guardpb.ListNodesResponse{}
	for _, n := range children(f.nodes, in.GetParent()+"/nodes/") {
		read, err := f.readNodeLocked(n.GetName())
		if err != nil {
			return nil, err
		}
		out.Nodes = append(out.Nodes, read)
	}
	return out, nil
}

func (f *fakeGuardServer) PatchNode(ctx context.Context, in *guardpb.PatchNodeRequest) (*guardpb.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	existing, err := f.readNodeLocked(in.GetName())
	if err != nil {
		return nil, err
	}
	next := proto.Clone(f.nodes[in.GetName()]).(*guardpb.Node)
	if err := applyMask(next, in.GetNode(), in.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if next.GetAddress() == "" {
		next.Address = existing.GetAddress()
	}
	switch {
	case next.GetAddress() != existing.GetAddress():
		return nil, status.Errorf(codes.InvalidArgument,
			"address cannot change after the node is created (it is %s); delete and re-create the node to give it a new address", existing.GetAddress())
	case next.GetState() != existing.GetState():
		return nil, status.Error(codes.InvalidArgument, "state changes only by enrolment and :revoke")
	case next.GetAuthCode() != "":
		return nil, status.Error(codes.InvalidArgument, "auth_code is output-only: use :issueAuthCode")
	case len(next.GetAllowedIps()) > 0 && !slices.Equal(next.GetAllowedIps(), existing.GetAllowedIps()):
		return nil, status.Error(codes.InvalidArgument, "allowed_ips is derived and may only echo the current value")
	case len(next.GetRoles()) > 0 && !slices.Equal(next.GetRoles(), existing.GetRoles()):
		return nil, status.Error(codes.InvalidArgument, "roles is derived from the node's owner, grants and the resources it serves")
	}
	next.PublicKey = strings.TrimSpace(next.GetPublicKey())
	switch {
	case next.GetPublicKey() == existing.GetPublicKey():
	case existing.GetState() == "REVOKED":
		return nil, status.Error(codes.InvalidArgument, "public_key cannot change on a revoked node; delete it and enrol again")
	case next.GetPublicKey() == "":
		return nil, status.Error(codes.InvalidArgument, "public_key cannot be cleared; use :issueAuthCode to rebind the node to a new key")
	case existing.GetState() == "PENDING":
		next.State, next.AuthCodeExpiresAt = "ACTIVE", nil
	}
	next.AllowedIps, next.Roles = nil, nil
	f.nodes[in.GetName()] = next
	return f.readNodeLocked(in.GetName())
}

func (f *fakeGuardServer) DeleteNode(ctx context.Context, in *guardpb.DeleteNodeRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.nodes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "node %q not found", in.GetName())
	}
	id := in.GetName()[strings.LastIndex(in.GetName(), "/")+1:]
	for _, r := range f.resources {
		if networkOf(r.GetName()) == networkOf(in.GetName()) && slices.Contains(r.GetServingNodeIds(), id) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"the node cannot be deleted while it serves resources: %s. Serve them from another node, or delete them, first", r.GetDisplayName())
		}
	}
	for from, targets := range f.grants {
		if slices.Contains(targets, in.GetName()) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"the node cannot be deleted while other nodes are granted to reach it: %s. Remove those grants first", from)
		}
	}
	delete(f.nodes, in.GetName())
	delete(f.grants, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- GuardNodeService: grants ---

func (f *fakeGuardServer) ListNodeGrants(ctx context.Context, in *guardpb.ListNodeGrantsRequest) (*guardpb.ListNodeGrantsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.nodes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "node %q not found", in.GetName())
	}
	out := &guardpb.ListNodeGrantsResponse{}
	for _, target := range f.grants[in.GetName()] {
		g := &guardpb.NodeGrant{Target: target, Kind: "node"}
		if r, ok := f.resources[target]; ok {
			g.Kind, g.DisplayName = "resource", r.GetDisplayName()
		} else {
			g.DisplayName = f.nodes[target].GetDisplayName()
		}
		out.Grants = append(out.Grants, g)
	}
	return out, nil
}

func (f *fakeGuardServer) AssociateGrantsToNode(ctx context.Context, in *guardpb.AssociateGrantsToNodeRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(ctx)
	if _, ok := f.nodes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "node %q not found", in.GetName())
	}
	network := networkOf(in.GetName())
	targets := slices.Clone(f.grants[in.GetName()])
	for _, t := range in.GetAssociation().GetSet() {
		_, isResource := f.resources[t]
		_, isNode := f.nodes[t]
		switch {
		case !strings.HasPrefix(t, network+"/"), !isResource && !isNode:
			return nil, status.Errorf(codes.InvalidArgument, "%s is not a resource or node of %s", t, network)
		case t == in.GetName():
			return nil, status.Error(codes.InvalidArgument, "a node cannot be granted itself")
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

// enrol binds a pending node to a key from the device, as the agent's
// enrolment would: it writes public_key and endpoint, which terraform did
// not set.
func (f *fakeGuardServer) enrol(name, publicKey, endpoint string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[name]
	n.PublicKey, n.Endpoint, n.State, n.AuthCodeExpiresAt = publicKey, endpoint, "ACTIVE", nil
}
