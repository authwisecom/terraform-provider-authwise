package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type secretResource struct{}

func NewSecret() resource.Resource {
	return &secretResource{}
}

func (*secretResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*secretResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaSecret(ctx)
}

func (*secretResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*secretResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*secretResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*secretResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
