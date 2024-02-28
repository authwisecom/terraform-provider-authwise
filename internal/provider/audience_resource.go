package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type audienceResource struct{}

func NewAudience() resource.Resource {
	return &audienceResource{}
}

type audienceModel struct {
	Id                  types.String `tfsdk:"id"`
	TenantId            types.String `tfsdk:"tenant_id"`
	Name                types.String `tfsdk:"name"`
	AppearanceProfileId types.String `tfsdk:"appearance_profile_id"`
	Description         types.String `tfsdk:"description"`
	Config              types.Object `tfsdk:"config"`
}

func (*audienceResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_audience"
}

func (*audienceResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaAudience(ctx)
}

func (*audienceResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data audienceModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*audienceResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*audienceResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*audienceResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
