package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type event struct{}

func NewEvent() resource.Resource {
	return &event{}
}

func (*event) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*event) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*event) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*event) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*event) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*event) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
