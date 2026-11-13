package provider

import (
	"context"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Node groups beyond their CRUD (train 2026.11.GFK.1, GK5): who is in a
// group, what the group may reach, and which resources a group serves. Each
// is an authoritative set over one Associate RPC, like
// authwise_guard_node_grants, on tfinfra's association runtime.

const (
	// guardNodeGroupServiceKey is GuardNodeGroupService unadapted: its
	// association RPCs. The adapted client (internal/guardadapt) is the
	// generated resource's, under "guard_node_group".
	guardNodeGroupServiceKey = "guard_node_group_service"

	guardNodeGroupMembersTypeName   = "guard_node_group_members"
	guardNodeGroupGrantsTypeName    = "guard_node_group_grants"
	guardResourceNodeGroupsTypeName = "guard_resource_node_groups"

	guardNodeGroupAttribute  = "node_group"
	guardNodesAttribute      = "nodes"
	guardResourceAttribute   = "resource"
	guardNodeGroupsAttribute = "node_groups"

	nodeGroupsCollection = "nodeGroups"
	resourcesCollection  = "resources"

	listPageSize = 200
)

var guardNetworkScope = tfruntime.NewScope("tenants", "networks")

// guardAssociationResource is one authoritative set over tfinfra's
// association runtime; build makes the runtime from the provider's data.
type guardAssociationResource struct {
	typeName        string
	entityAttribute string
	attribute       string
	description     string
	build           func(pd *tfruntime.ProviderData) *tfruntime.Association

	assoc *tfruntime.Association
}

func (r *guardAssociationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.typeName
}

func (r *guardAssociationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = tfruntime.AssociationSchema(r.entityAttribute, r.attribute)
	resp.Schema.MarkdownDescription = r.description
}

func (r *guardAssociationResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.assoc = r.build(pd)
	}
}

func (r *guardAssociationResource) configured(diags interface{ AddError(string, string) }) bool {
	if r.assoc == nil {
		diags.AddError(providerTypeName+"_"+r.typeName+" is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func (r *guardAssociationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Create(ctx, req, resp)
	}
}

func (r *guardAssociationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Read(ctx, req, resp)
	}
}

func (r *guardAssociationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Update(ctx, req, resp)
	}
}

func (r *guardAssociationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Delete(ctx, req, resp)
	}
}

func (r *guardAssociationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.ImportState(ctx, req, resp)
	}
}

// networkOf is the network part of a name under a network,
// tenants/{t}/networks/{n}/….
func networkOf(name string) string {
	ids, _, err := guardNetworkScope.ParseName(nodeGroupsCollection, name)
	if err != nil {
		return ""
	}
	parent, _ := guardNetworkScope.ComposeParent(ids)
	return parent
}

// authwise_guard_node_group_members: AssociateNodesToNodeGroup, read back
// through ListNodes filtered on the group.
func newGuardNodeGroupMembersResource() resource.Resource {
	return &guardAssociationResource{
		typeName:        guardNodeGroupMembersTypeName,
		entityAttribute: guardNodeGroupAttribute,
		attribute:       guardNodesAttribute,
		description: "The nodes of a Guard node group, by full name (`authwise_guard_node.<name>.name`), as an authoritative set: a member added out of band is removed on the next apply. " +
			"A node may be in any number of groups.\n\n" +
			"**Not for groups that credentials fill.** A node that joins with an `authwise_guard_enrollment_key` or under an `authwise_guard_cluster_trust_rule` naming the group is added to it at join, and this resource would remove it on the next apply. " +
			"Use it for groups of nodes you register yourself.",
		build: func(pd *tfruntime.ProviderData) *tfruntime.Association {
			groups, ok := pd.Clients[guardNodeGroupServiceKey].(guardpb.GuardNodeGroupServiceClient)
			if !ok {
				return nil
			}
			nodes, ok := pd.Clients[guardClientKey].(guardpb.GuardControlServiceClient)
			if !ok {
				return nil
			}
			return tfruntime.NewAssociation(tfruntime.AssociationParams{
				TypeName:        guardNodeGroupMembersTypeName,
				EntityAttribute: guardNodeGroupAttribute,
				Attribute:       guardNodesAttribute,
				Scope:           guardNetworkScope,
				Collection:      nodeGroupsCollection,
				Client: tfruntime.AssociationClient{
					Associate: func(ctx context.Context, name string, set, remove []string) error {
						_, err := groups.AssociateNodesToNodeGroup(ctx, &guardpb.AssociateNodesToNodeGroupRequest{
							Name:        name,
							Association: &guardpb.AssociationRequest{Set: set, Remove: remove},
						})
						return err
					},
					ListBy: func(ctx context.Context, name, pageToken string) ([]string, string, error) {
						out, err := nodes.ListNodes(ctx, &guardpb.ListNodesRequest{
							Parent:    networkOf(name),
							Filter:    `node_group="` + name + `"`,
							PageSize:  listPageSize,
							PageToken: pageToken,
						})
						if err != nil {
							return nil, "", err
						}
						names := make([]string, 0, len(out.GetNodes()))
						for _, n := range out.GetNodes() {
							names = append(names, n.GetName())
						}
						return names, out.GetNextPageToken(), nil
					},
				},
			})
		},
	}
}

// authwise_guard_node_group_grants: AssociateGrantsToNodeGroup and
// ListNodeGroupGrants, as authwise_guard_node_grants is for one node.
func newGuardNodeGroupGrantsResource() resource.Resource {
	return &guardAssociationResource{
		typeName:        guardNodeGroupGrantsTypeName,
		entityAttribute: guardNodeGroupAttribute,
		attribute:       guardGrantsAttribute,
		description: "What every member of a Guard node group may reach: the full names of resources, nodes and node groups of its network, as an authoritative set. " +
			"A granted node group means every member of it. A member's reach is its own grants (`authwise_guard_node_grants`), its groups' grants, and the members of any group granted.\n\n" +
			"A grant made out of band, such as one made in the Guard console, is removed on the next apply.",
		build: func(pd *tfruntime.ProviderData) *tfruntime.Association {
			groups, ok := pd.Clients[guardNodeGroupServiceKey].(guardpb.GuardNodeGroupServiceClient)
			if !ok {
				return nil
			}
			return tfruntime.NewAssociation(tfruntime.AssociationParams{
				TypeName:        guardNodeGroupGrantsTypeName,
				EntityAttribute: guardNodeGroupAttribute,
				Attribute:       guardGrantsAttribute,
				Scope:           guardNetworkScope,
				Collection:      nodeGroupsCollection,
				Client: tfruntime.AssociationClient{
					Associate: func(ctx context.Context, name string, set, remove []string) error {
						_, err := groups.AssociateGrantsToNodeGroup(ctx, &guardpb.AssociateGrantsToNodeGroupRequest{
							Name:        name,
							Association: &guardpb.AssociationRequest{Set: set, Remove: remove},
						})
						return err
					},
					ListBy: func(ctx context.Context, name, _ string) ([]string, string, error) {
						out, err := groups.ListNodeGroupGrants(ctx, &guardpb.ListNodeGroupGrantsRequest{Name: name})
						if err != nil {
							return nil, "", err
						}
						targets := make([]string, 0, len(out.GetGrants()))
						for _, g := range out.GetGrants() {
							targets = append(targets, g.GetTarget())
						}
						return targets, "", nil
					},
				},
			})
		},
	}
}

// authwise_guard_resource_node_groups: the groups that serve a resource.
// guard-control's AssociateNodesToResource takes node and node group names
// alike; this resource writes group names only and reads the resource's
// serving_node_groups, so it and authwise_guard_resource_nodes (node names,
// read through ListNodesByResource) each own their half.
func newGuardResourceNodeGroupsResource() resource.Resource {
	return &guardAssociationResource{
		typeName:        guardResourceNodeGroupsTypeName,
		entityAttribute: guardResourceAttribute,
		attribute:       guardNodeGroupsAttribute,
		description: "The node groups that serve a Guard resource, by full name (`authwise_guard_node_group.<name>.name`), as an authoritative set: every member of a group serves it, including nodes that join the group later. " +
			"The nodes that serve it by name are `authwise_guard_resource_nodes`; the two sets are separate and may be used together.\n\n" +
			"A translated resource (`authwise_guard_resource.translated`) is served only by nodes that report `app-translate`; a group with a member that does not is refused (`NODE_LACKS_CAPABILITY`).",
		build: func(pd *tfruntime.ProviderData) *tfruntime.Association {
			client, ok := pd.Clients[guardClientKey].(guardpb.GuardControlServiceClient)
			if !ok {
				return nil
			}
			return tfruntime.NewAssociation(tfruntime.AssociationParams{
				TypeName:        guardResourceNodeGroupsTypeName,
				EntityAttribute: guardResourceAttribute,
				Attribute:       guardNodeGroupsAttribute,
				Scope:           guardNetworkScope,
				Collection:      resourcesCollection,
				Client: tfruntime.AssociationClient{
					Associate: func(ctx context.Context, name string, set, remove []string) error {
						_, err := client.AssociateNodesToResource(ctx, &guardpb.AssociateNodesToResourceRequest{
							Name:        name,
							Association: &guardpb.AssociationRequest{Set: set, Remove: remove},
						})
						return err
					},
					ListBy: func(ctx context.Context, name, _ string) ([]string, string, error) {
						out, err := client.GetResource(ctx, &guardpb.GetResourceRequest{Name: name})
						if err != nil {
							return nil, "", err
						}
						return out.GetServingNodeGroups(), "", nil
					},
				},
			})
		},
	}
}
