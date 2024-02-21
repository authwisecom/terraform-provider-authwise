package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type audience struct{}

func NewAudience() resource.Resource {
	return &audience{}
}

func (*audience) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*audience) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*audience) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*audience) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*audience) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*audience) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
