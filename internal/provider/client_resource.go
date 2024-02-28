package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
)

type clientResource struct{}

func NewClient() resource.Resource {
	return &clientResource{}
}

type clientModel struct {
	Id                  types.String `tfsdk:"id"`
	AudienceId          types.String `tfsdk:"audience_id"`
	AppearanceProfileId types.String `tfsdk:"appearance_profile_id"`
	Name                types.String `tfsdk:"name"`
	Alias               types.String `tfsdk:"alias"`
	GrantType           types.String `tfsdk:"grant_type"`
	LoginUrl            types.String `tfsdk:"login_url"`
	LogoId              types.String `tfsdk:"logo_id"`
	Config              types.Object `tfsdk:"config"`
	Metadata            types.Object `tfsdk:"metadata"`
}

func (*clientResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_client"
}

func (*clientResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaClient(ctx)
}

func (*clientResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data clientModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}
}

func (*clientResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*clientResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*clientResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
