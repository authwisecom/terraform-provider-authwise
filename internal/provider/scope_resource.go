package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type scopeResource struct{}

func NewScope() resource.Resource {
	return &scopeResource{}
}

func (*scopeResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*scopeResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaScope(ctx)
}

func (*scopeResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*scopeResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*scopeResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*scopeResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
