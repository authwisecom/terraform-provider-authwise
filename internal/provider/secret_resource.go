package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type secretResource struct{}

func NewSecret() resource.Resource {
	return &secretResource{}
}

type secretModel struct {
	Id       types.String `tfsdk:"id"`
	TenantId types.String `tfsdk:"tenant_id"`
	Name     types.String `tfsdk:"name"`
	Encoding types.Object `tfsdk:"encoding"`
	Value    types.String `tfsdk:"value"`
}

func (*secretResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_secret"
}

func (*secretResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaSecret(ctx)
}

func (*secretResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data secretModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*secretResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*secretResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*secretResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
