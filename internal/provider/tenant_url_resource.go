package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type tenantUrlResource struct{}

func NewTenantUrl() resource.Resource {
	return &tenantUrlResource{}
}

func (*tenantUrlResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*tenantUrlResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaTenantUrl(ctx)
}

func (*tenantUrlResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*tenantUrlResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*tenantUrlResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*tenantUrlResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
