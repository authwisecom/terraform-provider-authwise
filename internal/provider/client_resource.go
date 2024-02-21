package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type client struct{}

func NewClient() resource.Resource {
	return &client{}
}

func (*client) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*client) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*client) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*client) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*client) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*client) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
