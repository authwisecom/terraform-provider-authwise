package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type appearanceProfileResource struct{}

func NewAppearanceProfile() resource.Resource {
	return &appearanceProfileResource{}
}

func (*appearanceProfileResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*appearanceProfileResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaAppearanceProfile(ctx)
}

func (*appearanceProfileResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*appearanceProfileResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*appearanceProfileResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*appearanceProfileResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
