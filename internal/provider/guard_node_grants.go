package provider

import (
	"context"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// guardNodeClientKey is the ProviderData.Clients key of guard-control's
// GuardNodeService client.
const guardNodeClientKey = "guard_node"

// authwise_guard_node_grants is what a node may reach: resources and nodes
// of its network, as an authoritative set (#32 stage 2). It is tfinfra's
// association runtime over GuardNodeService's AssociateGrantsToNode and
// ListNodeGrants, which the generator's Associate cannot target: they live
// on a second service, and the list answers grants (a target name and its
// kind) rather than entities, in one page.
type guardNodeGrantsResource struct {
	assoc *tfruntime.Association
}

func newGuardNodeGrantsResource() resource.Resource {
	return &guardNodeGrantsResource{}
}

const (
	guardNodeGrantsTypeName = "guard_node_grants"
	guardNodeAttribute      = "guard_node"
	guardGrantsAttribute    = "grants"
)

func (r *guardNodeGrantsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardNodeGrantsTypeName
}

func (r *guardNodeGrantsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = tfruntime.AssociationSchema(guardNodeAttribute, guardGrantsAttribute)
	resp.Schema.MarkdownDescription = "What a Guard node may reach: the full names of resources and nodes of its network, as an authoritative set. " +
		"A grant made out of band, such as one an invite gave the node, is removed on the next apply."
}

func (r *guardNodeGrantsResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {

	pd, ok := req.ProviderData.(*tfruntime.ProviderData)
	if !ok {
		return
	}
	client, ok := pd.Clients[guardNodeClientKey].(guardpb.GuardNodeServiceClient)
	if !ok {
		return
	}

	r.assoc = tfruntime.NewAssociation(tfruntime.AssociationParams{
		TypeName:        guardNodeGrantsTypeName,
		EntityAttribute: guardNodeAttribute,
		Attribute:       guardGrantsAttribute,
		Scope:           tfruntime.NewScope("tenants", "networks"),
		Collection:      "nodes",
		Client: tfruntime.AssociationClient{
			Associate: func(ctx context.Context, name string, set, remove []string) error {
				_, err := client.AssociateGrantsToNode(ctx, &guardpb.AssociateGrantsToNodeRequest{
					Name:        name,
					Association: &guardpb.AssociationRequest{Set: set, Remove: remove},
				})
				return err
			},
			ListBy: func(ctx context.Context, name, _ string) ([]string, string, error) {
				out, err := client.ListNodeGrants(ctx, &guardpb.ListNodeGrantsRequest{Name: name})
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
}

func (r *guardNodeGrantsResource) configured(diags interface{ AddError(string, string) }) bool {
	if r.assoc == nil {
		diags.AddError("authwise_guard_node_grants is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func (r *guardNodeGrantsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Create(ctx, req, resp)
	}
}

func (r *guardNodeGrantsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Read(ctx, req, resp)
	}
}

func (r *guardNodeGrantsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Update(ctx, req, resp)
	}
}

func (r *guardNodeGrantsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.Delete(ctx, req, resp)
	}
}

func (r *guardNodeGrantsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.configured(&resp.Diagnostics) {
		r.assoc.ImportState(ctx, req, resp)
	}
}
