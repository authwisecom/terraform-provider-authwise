package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type assetResource struct{}

func NewAsset() resource.Resource {
	return &assetResource{}
}

func (*assetResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*assetResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaAsset(ctx)
}

func (*assetResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*assetResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*assetResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*assetResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
