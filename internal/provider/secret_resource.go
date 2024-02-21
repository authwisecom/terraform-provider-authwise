package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type secret struct{}

func NewSecret() resource.Resource {
	return &secret{}
}

func (*secret) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*secret) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*secret) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*secret) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*secret) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*secret) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
