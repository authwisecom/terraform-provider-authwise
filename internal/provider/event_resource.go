package provider

import (
	"context"
	"fmt"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	v1alpha1 "gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1"
	v1alpha11 "gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1"
)

type eventResource struct {
	client v1alpha1.AuthwiseManagementServiceClient
}

func NewEvent() resource.Resource {
	return &eventResource{}
}

func (r *eventResource) Configure(ctx context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *eventResource) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_event"
}

func (r *eventResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = GenSchemaEvent(ctx)
}

func (r *eventResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data v1alpha11.Event

	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)

	if response.Diagnostics.HasError() {
		return
	}

	cr := &v1alpha1.CreateEventRequest{}
}

func (r *eventResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
}

func (r *eventResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
}

func (r *eventResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
}
