package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type eventResource struct{}

func NewEvent() resource.Resource {
	return &eventResource{}
}

func (*eventResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*eventResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaEvent(ctx)
}

func (*eventResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*eventResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*eventResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*eventResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
