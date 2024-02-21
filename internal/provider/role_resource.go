package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type roleResource struct{}

func NewRole() resource.Resource {
	return &roleResource{}
}

func (*roleResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*roleResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaRole(ctx)
}

func (*roleResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*roleResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*roleResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*roleResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
