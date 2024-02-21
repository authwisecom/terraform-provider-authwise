package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type themeResource struct{}

func NewTheme() resource.Resource {
	return &themeResource{}
}

func (*themeResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*themeResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaTheme(ctx)
}

func (*themeResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*themeResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*themeResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*themeResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
