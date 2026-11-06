package provider

import (
	"context"
	"strings"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	// guardRelayPoolClientKey is GuardRelayPoolService: relay credentials,
	// a tenant's allowed pools and the operator's access per tenant
	// (apis v0.59.0, tf#66).
	guardRelayPoolClientKey = "guard_relay_pool"

	guardRelayCredentialTypeName = "guard_relay_credential"

	// poolRelaysCollection is an estate pool relay's collection,
	// pool-relays/rl-…; any other relay name is a network's own relay.
	poolRelaysCollection = "pool-relays"
)

var (
	_ resource.Resource              = &guardRelayCredentialResource{}
	_ resource.ResourceWithConfigure = &guardRelayCredentialResource{}
)

// guardRelayPoolClientFrom is the GuardRelayPoolService client in the
// provider's data, or nil before the provider is configured.
func guardRelayPoolClientFrom(providerData any) guardpb.GuardRelayPoolServiceClient {
	pd, ok := providerData.(*tfruntime.ProviderData)
	if !ok {
		return nil
	}
	client, _ := pd.Clients[guardRelayPoolClientKey].(guardpb.GuardRelayPoolServiceClient)
	return client
}

// guardRelayCredentialResource is authwise_guard_relay_credential (tf#66):
// the bearer a relay polls guard-control with, from :issueCredential. The
// credential is shown once and a second issue rotates it, so it is issued
// on create only and kept in state; rotating is a replacement, driven by
// `rotation`. Destroy revokes nothing (revoking is an operator's action on a
// compromised relay), and there is no import: an import would have to issue.
//
// It is a resource of its own rather than attributes of the relay: the
// relay's lifecycle is not the credential's, and a relay whose credential
// is held elsewhere is managed without it.
type guardRelayCredentialResource struct {
	client guardpb.GuardRelayPoolServiceClient
	relays guardpb.GuardControlServiceClient
}

type guardRelayCredentialModel struct {
	Relay      types.String `tfsdk:"relay"`
	Rotation   types.String `tfsdk:"rotation"`
	Credential types.String `tfsdk:"credential"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func newGuardRelayCredentialResource() resource.Resource {
	return &guardRelayCredentialResource{}
}

func (r *guardRelayCredentialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + guardRelayCredentialTypeName
}

func (r *guardRelayCredentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A relay's credential: the `grk_…` bearer it reports to guard-control with, for a pool relay (`authwise_guard_pool_relay`) or a network's own relay (`authwise_guard_relay`) that polls control for its networks. " +
			"Hand it to the relay's VM, which writes it to its token file as it is.\n\n" +
			"guard-control shows a credential once, so it is issued when this resource is created and kept in state: **treat the state as sensitive.** " +
			"Issuing again rotates it, so a refresh never issues. To rotate, change `rotation`: the resource is replaced and a new credential issued; the previous one keeps working until the relay first uses the new one.\n\n" +
			"**Destroy revokes nothing**: the credential keeps working until an operator revokes it (awctl or the console) or the relay is deleted, which revokes it. " +
			"A credential guard-control no longer holds (revoked, or the relay gone) drops out of state at refresh, and the next apply issues a new one.\n\n" +
			"There is no import: importing would have to issue. A pool relay's credential is an operator's to issue; a network relay's needs `guardcontrol.relays.update`.",
		Attributes: map[string]schema.Attribute{
			"relay": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The relay's name: `authwise_guard_pool_relay.<name>.name` (`pool-relays/rl-…`) or `authwise_guard_relay.<name>.name` (`tenants/t-…/networks/n-…/relays/rl-…`). " +
					"Changing it replaces the resource.",
				PlanModifiers: replace,
			},
			"rotation": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Any value; changing it replaces the resource, which issues a new credential. " +
					"For example a date, or `time_rotating`'s `id` for rotation on a schedule.",
				PlanModifiers: replace,
			},
			"credential": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "The relay's credential, `grk_…`: written to the relay's token file as it is.",
				PlanModifiers:       keep,
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the credential was issued, RFC 3339.",
				PlanModifiers:       keep,
			},
		},
	}
}

func (r *guardRelayCredentialResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = guardRelayPoolClientFrom(req.ProviderData)
	r.relays = guardClientFrom(req.ProviderData)
}

func (r *guardRelayCredentialResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil || r.relays == nil {
		diags.AddError("authwise_guard_relay_credential is not configured", "Configure was not called with the provider's data")
		return false
	}
	return true
}

func isPoolRelay(name string) bool {
	return strings.HasPrefix(name, poolRelaysCollection+"/")
}

func (r *guardRelayCredentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan guardRelayCredentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	relay := plan.Relay.ValueString()
	var (
		out *guardpb.RelayCredential
		err error
	)
	if isPoolRelay(relay) {
		out, err = r.client.IssuePoolRelayCredential(ctx, &guardpb.IssuePoolRelayCredentialRequest{Name: relay})
	} else {
		out, err = r.client.IssueRelayCredential(ctx, &guardpb.IssueRelayCredentialRequest{Name: relay})
	}
	if err != nil {
		resp.Diagnostics.AddError("issuing a credential for "+relay+" failed", err.Error())
		return
	}

	plan.Credential = types.StringValue(out.GetCredential())
	plan.CreatedAt = timestampString(out.GetCreatedAt())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read keeps the credential, which no read returns, while guard-control
// still holds one for the relay; a relay gone or without a credential
// (revoked) drops the resource, so the next apply issues anew.
func (r *guardRelayCredentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state guardRelayCredentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	relay := state.Relay.ValueString()
	var (
		held bool
		err  error
	)
	if isPoolRelay(relay) {
		var out *guardpb.PoolRelay
		out, err = r.relays.GetPoolRelay(ctx, &guardpb.GetPoolRelayRequest{Name: relay})
		held = out.GetHasCredential()
	} else {
		var out *guardpb.Relay
		out, err = r.relays.GetRelay(ctx, &guardpb.GetRelayRequest{Name: relay})
		held = out.GetHasCredential()
	}
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading relay "+relay+" failed", err.Error())
		return
	}
	if !held {
		resp.State.RemoveResource(ctx)
	}
}

// Update has nothing to write: every argument replaces.
func (r *guardRelayCredentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan guardRelayCredentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete revokes nothing.
func (r *guardRelayCredentialResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
