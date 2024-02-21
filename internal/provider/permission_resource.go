package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type permissionResource struct{}

func NewPermission() resource.Resource {
	return &permissionResource{}
}

func (*permissionResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*permissionResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaPermission(ctx)
}

func (*permissionResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*permissionResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*permissionResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*permissionResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
