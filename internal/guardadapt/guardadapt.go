// Package guardadapt fits guard-control's hand-authored services to the
// generator's client shape (train 2026.11.GFK.1, GK5).
//
// tfinfra reads a client interface by reflection and expects AIP's Patch
// shape: a request with `name`, the entity and `update_mask`. The services
// guard-control writes by hand beside the generated GuardControlService
// (GuardNodeGroupService, GuardTrustedIssuerService) carry the name inside the
// entity instead (`UpdateNodeGroupRequest{node_group, update_mask}`). Each
// client here is the generated one with its Update re-exposed as Patch over a
// request of the expected shape. Get, List, Create and Delete pass through
// with the pb requests unchanged.
package guardadapt

import (
	"context"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// PatchNodeGroupRequest is UpdateNodeGroupRequest with the name beside the
// entity. The protobuf tags are what the generator reads the field roles
// from; the type is never marshalled.
type PatchNodeGroupRequest struct {
	Name       string                 `protobuf:"bytes,1,opt,name=name,proto3"`
	NodeGroup  *guardpb.NodeGroup     `protobuf:"bytes,2,opt,name=node_group,proto3"`
	UpdateMask *fieldmaskpb.FieldMask `protobuf:"bytes,3,opt,name=update_mask,proto3"`
}

// NodeGroupClient is GuardNodeGroupService's CRUD in the generator's shape.
type NodeGroupClient interface {
	GetNodeGroup(ctx context.Context, in *guardpb.GetNodeGroupRequest, opts ...grpc.CallOption) (*guardpb.NodeGroup, error)
	ListNodeGroups(ctx context.Context, in *guardpb.ListNodeGroupsRequest, opts ...grpc.CallOption) (*guardpb.ListNodeGroupsResponse, error)
	CreateNodeGroup(ctx context.Context, in *guardpb.CreateNodeGroupRequest, opts ...grpc.CallOption) (*guardpb.NodeGroup, error)
	PatchNodeGroup(ctx context.Context, in *PatchNodeGroupRequest, opts ...grpc.CallOption) (*guardpb.NodeGroup, error)
	DeleteNodeGroup(ctx context.Context, in *guardpb.DeleteNodeGroupRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type nodeGroupClient struct {
	guardpb.GuardNodeGroupServiceClient
}

// NewNodeGroupClient adapts guard-control's node group client.
func NewNodeGroupClient(c guardpb.GuardNodeGroupServiceClient) NodeGroupClient {
	return nodeGroupClient{c}
}

func (c nodeGroupClient) PatchNodeGroup(ctx context.Context, in *PatchNodeGroupRequest, opts ...grpc.CallOption) (*guardpb.NodeGroup, error) {
	g := in.NodeGroup
	if g == nil {
		g = &guardpb.NodeGroup{}
	}
	g.Name = in.Name
	return c.UpdateNodeGroup(ctx, &guardpb.UpdateNodeGroupRequest{NodeGroup: g, UpdateMask: in.UpdateMask}, opts...)
}

// PatchTrustedIssuerRuleRequest is UpdateTrustedIssuerRuleRequest with the
// name beside the entity.
type PatchTrustedIssuerRuleRequest struct {
	Name       string                     `protobuf:"bytes,1,opt,name=name,proto3"`
	Rule       *guardpb.TrustedIssuerRule `protobuf:"bytes,2,opt,name=rule,proto3"`
	UpdateMask *fieldmaskpb.FieldMask     `protobuf:"bytes,3,opt,name=update_mask,proto3"`
}

// TrustedIssuerRuleClient is the rule half of GuardTrustedIssuerService in the
// generator's shape. The issuer itself is hand-written: its keys are a oneof.
type TrustedIssuerRuleClient interface {
	GetTrustedIssuerRule(ctx context.Context, in *guardpb.GetTrustedIssuerRuleRequest, opts ...grpc.CallOption) (*guardpb.TrustedIssuerRule, error)
	ListTrustedIssuerRules(ctx context.Context, in *guardpb.ListTrustedIssuerRulesRequest, opts ...grpc.CallOption) (*guardpb.ListTrustedIssuerRulesResponse, error)
	CreateTrustedIssuerRule(ctx context.Context, in *guardpb.CreateTrustedIssuerRuleRequest, opts ...grpc.CallOption) (*guardpb.TrustedIssuerRule, error)
	PatchTrustedIssuerRule(ctx context.Context, in *PatchTrustedIssuerRuleRequest, opts ...grpc.CallOption) (*guardpb.TrustedIssuerRule, error)
	DeleteTrustedIssuerRule(ctx context.Context, in *guardpb.DeleteTrustedIssuerRuleRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type trustedIssuerRuleClient struct {
	guardpb.GuardTrustedIssuerServiceClient
}

// NewTrustedIssuerRuleClient adapts guard-control's trusted issuer client.
func NewTrustedIssuerRuleClient(c guardpb.GuardTrustedIssuerServiceClient) TrustedIssuerRuleClient {
	return trustedIssuerRuleClient{c}
}

func (c trustedIssuerRuleClient) PatchTrustedIssuerRule(ctx context.Context, in *PatchTrustedIssuerRuleRequest, opts ...grpc.CallOption) (*guardpb.TrustedIssuerRule, error) {
	r := in.Rule
	if r == nil {
		r = &guardpb.TrustedIssuerRule{}
	}
	r.Name = in.Name
	return c.UpdateTrustedIssuerRule(ctx, &guardpb.UpdateTrustedIssuerRuleRequest{Rule: r, UpdateMask: in.UpdateMask}, opts...)
}
