package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type realmResource struct{}

func NewRealm() resource.Resource {
	return &realmResource{}
}

func (*realmResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*realmResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaRealm(ctx)
}

func (*realmResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*realmResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*realmResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*realmResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
