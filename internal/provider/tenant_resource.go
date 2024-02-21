package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type tenant struct{}

func NewTenant() resource.Resource {
	return &tenant{}
}

func (*tenant) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*tenant) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*tenant) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*tenant) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*tenant) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*tenant) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
