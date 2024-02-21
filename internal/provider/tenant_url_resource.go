package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type tenantUrl struct{}

func NewTenantUrl() resource.Resource {
	return &tenantUrl{}
}

func (*tenantUrl) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*tenantUrl) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*tenantUrl) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*tenantUrl) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*tenantUrl) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*tenantUrl) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
