package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type providerResource struct{}

func NewProvider() resource.Resource {
	return &providerResource{}
}

func (*providerResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*providerResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaProvider(ctx)
}

func (*providerResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*providerResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*providerResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*providerResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
