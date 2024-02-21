package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type role struct{}

func NewRole() resource.Resource {
	return &role{}
}

func (*role) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*role) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*role) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*role) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*role) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*role) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
