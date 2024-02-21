package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type theme struct{}

func NewTheme() resource.Resource {
	return &theme{}
}

func (*theme) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*theme) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*theme) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*theme) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*theme) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*theme) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
