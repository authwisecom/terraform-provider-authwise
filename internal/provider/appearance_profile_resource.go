package provider

import (
	"context"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type appearanceProfile struct{}

func NewAppearanceProfile() resource.Resource {
	return &appearanceProfile{}
}

func (*appearanceProfile) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
}

func (*appearanceProfile) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
}

func (*appearanceProfile) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
}

func (*appearanceProfile) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (*appearanceProfile) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (*appearanceProfile) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
