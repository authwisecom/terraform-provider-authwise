package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type themeResource struct{}

func NewTheme() resource.Resource {
	return &themeResource{}
}

type themeModel struct {
	Id                   types.String `tfsdk:"id"`
	TenantId             types.String `tfsdk:"tenant_id"`
	Name                 types.String `tfsdk:"name"`
	Stylesheet           types.String `tfsdk:"stylesheet"`
	StylesheetAttributes types.Object `tfsdk:"stylesheet_attributes"`
	Content              types.Object `tfsdk:"content"`
}

func (*themeResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_theme"
}

func (*themeResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaTheme(ctx)
}

func (*themeResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data themeModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*themeResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*themeResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*themeResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
