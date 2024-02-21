package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type permission struct{}

func NewPermission() resource.Resource {
	return &permission{}
}

func (*permission) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*permission) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*permission) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*permission) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*permission) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*permission) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
