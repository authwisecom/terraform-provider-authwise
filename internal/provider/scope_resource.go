package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type scope struct{}

func NewScope() resource.Resource {
	return &scope{}
}

func (*scope) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*scope) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*scope) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*scope) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*scope) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*scope) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
