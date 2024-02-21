package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type tenantResource struct{}

func NewTenant() resource.Resource {
	return &tenantResource{}
}

func (*tenantResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*tenantResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaTenant(ctx)
}

func (*tenantResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*tenantResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*tenantResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*tenantResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
