package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type eventResource struct{}

func NewEvent() resource.Resource {
	return &eventResource{}
}

type eventModel struct {
	Id            types.String `tfsdk:"id"`
	TenantId      types.String `tfsdk:"tenant_id"`
	RequestId     types.String `tfsdk:"request_id"`
	ClientId      types.String `tfsdk:"client_id"`
	RealmId       types.String `tfsdk:"realm_id"`
	UserId        types.String `tfsdk:"user_id"`
	SessionId     types.String `tfsdk:"session_id"`
	InteractionId types.String `tfsdk:"interaction_id"`
	EventType     types.String `tfsdk:"event_type"`
	EventMessage  types.String `tfsdk:"event_message"`
	StartState    types.String `tfsdk:"start_state"`
	EndState      types.String `tfsdk:"end_state"`
	StartTime     types.Object `tfsdk:"start_time"`
	EndTime       types.Object `tfsdk:"end_time"`
	Attributes    types.Object `tfsdk:"attributes"`
	CreatedAt     types.Object `tfsdk:"created_at"`
}

func (*eventResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_event"
}

func (*eventResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaEvent(ctx)
}

func (*eventResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data eventModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*eventResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*eventResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*eventResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
