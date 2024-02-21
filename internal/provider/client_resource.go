package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type clientResource struct{}

func NewClient() resource.Resource {
	return &clientResource{}
}

func (*clientResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*clientResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaClient(ctx)
}

func (*clientResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*clientResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*clientResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*clientResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
