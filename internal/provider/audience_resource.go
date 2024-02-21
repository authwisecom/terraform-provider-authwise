package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type audienceResource struct{}

func NewAudience() resource.Resource {
	return &audienceResource{}
}

func (*audienceResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*audienceResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaAudience(ctx)
}

func (*audienceResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*audienceResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*audienceResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*audienceResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
