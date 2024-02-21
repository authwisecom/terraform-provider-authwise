package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type asset struct{}

func NewAsset() resource.Resource {
	return &asset{}
}

func (*asset) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*asset) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*asset) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*asset) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*asset) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*asset) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
