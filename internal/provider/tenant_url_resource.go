package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type tenantUrlResource struct{}

func NewTenantUrl() resource.Resource {
	return &tenantUrlResource{}
}

type tenantUrlModel struct {
	Id       types.String `tfsdk:"id"`
	TenantId types.String `tfsdk:"tenant_id"`
	Config   types.Object `tfsdk:"config"`
}

func (*tenantUrlResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_tenant_url"
}

func (*tenantUrlResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaTenantUrl(ctx)
}

func (*tenantUrlResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data tenantUrlModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*tenantUrlResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*tenantUrlResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*tenantUrlResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
