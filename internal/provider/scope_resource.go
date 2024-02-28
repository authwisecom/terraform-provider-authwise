package provider

import (
	"context"
	"fmt"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	v1alpha1 "gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1"
	v1alpha11 "gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1"
)

type scopeResource struct {
	client v1alpha1.AuthwiseManagementServiceClient
}

func NewScope() resource.Resource {
	return &scopeResource{}
}

func (r *scopeResource) Configure(ctx context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
	if request.ProviderData == nil {
		return
	}
	client, ok := request.ProviderData.(v1alpha1.AuthwiseManagementServiceClient)
	if !ok {
		response.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *v1alpha12.AuthwiseManagementServiceClient, got: %T. Please report this issue to the provider developers.", request.ProviderData))
		return
	}
	r.client = client
}

func (r *scopeResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_scope"
}

func (r *scopeResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaScope(ctx)
}

func (r *scopeResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data v1alpha11.Scope

	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)

	if response.Diagnostics.HasError() {
		return
	}

	cr := &v1alpha1.CreateScopeRequest{}
}

func (r *scopeResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (r *scopeResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (r *scopeResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
