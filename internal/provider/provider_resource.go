package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type provider struct{}

func NewProvider() resource.Resource {
	return &provider{}
}

func (*provider) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*provider) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*provider) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*provider) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*provider) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*provider) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
