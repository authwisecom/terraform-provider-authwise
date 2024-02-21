package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type realm struct{}

func NewRealm() resource.Resource {
	return &realm{}
}

func (*realm) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*realm) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*realm) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*realm) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*realm) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*realm) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
