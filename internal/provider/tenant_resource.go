package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type tenantResource struct{}

func NewTenant() resource.Resource {
	return &tenantResource{}
}

type tenantModel struct {
	Id                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	AppearanceProfileId types.String `tfsdk:"appearance_profile_id"`
	Config              types.Object `tfsdk:"config"`
}

func (*tenantResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_tenant"
}

func (*tenantResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaTenant(ctx)
}

func (*tenantResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data tenantModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*tenantResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*tenantResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*tenantResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
