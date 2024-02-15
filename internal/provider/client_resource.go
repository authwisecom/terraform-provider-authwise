// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	v1alpha12 "gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1"
	v1alpha13 "gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1"
	"google.golang.org/protobuf/types/known/anypb"
	"terraform-provider-authwise/internal/model/authwise/types/core/v1alpha1"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ClientResource{}
var _ resource.ResourceWithImportState = &ClientResource{}

func NewClientResource() resource.Resource {
	return &ClientResource{}
}

// ClientResource defines the resource implementation.
type ClientResource struct {
	client v1alpha12.AuthwiseManagementServiceClient
}

func (r *ClientResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_client"
}

func (r *ClientResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = v1alpha1.GenSchemaClient(ctx)
}

func (r *ClientResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(v1alpha12.AuthwiseManagementServiceClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *v1alpha12.AuthwiseManagementServiceClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *ClientResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data v1alpha1.ClientModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create client, got error: %s", err))
	//     return
	// }

	c := &structpb.Struct{}

	diags := data.Config.As(ctx, c, basetypes.ObjectAsOptions{})

	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	config, err := anypb.New(c)

	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Client",
			err.Error(),
		)
		return
	}

	metadata := &structpb.Struct{}

	diags2 := data.Config.As(ctx, metadata, basetypes.ObjectAsOptions{})

	resp.Diagnostics.Append(diags2...)
	if diags2.HasError() {
		return
	}

	clientReq := &v1alpha12.CreateClientRequest{
		Client: &v1alpha13.Client{
			Id:                  data.Id.ValueString(),
			AudienceId:          data.AudienceId.ValueString(),
			AppearanceProfileId: data.AppearanceProfileId.ValueString(),
			Name:                data.Name.ValueString(),
			Alias:               data.Alias.ValueString(),
			GrantType:           data.GrantType.ValueString(),
			LoginUrl:            data.LoginUrl.ValueString(),
			LogoId:              data.LogoId.ValueString(),
			Config:              config,
			Metadata:            metadata,
		},
	}

	clientResp, err := r.client.CreateClient(ctx, clientReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Client",
			err.Error(),
		)
		return
	}

	// For the purposes of this client code, hardcoding a response value to
	// save into the Terraform state.
	data.Id = types.StringValue(clientResp.Id)
	data.Name = types.StringValue(clientResp.Name)
	data.Alias = types.StringValue(clientResp.Alias)
	data.LoginUrl = types.StringValue(clientResp.LoginUrl)
	data.GrantType = types.StringValue(clientResp.GrantType)
	data.LogoId = types.StringValue(clientResp.LogoId)
	data.AudienceId = types.StringValue(clientResp.AudienceId)
	data.AppearanceProfileId = types.StringValue(clientResp.AppearanceProfileId)

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created client")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ClientResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data v1alpha1.ClientModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read client, got error: %s", err))
	//     return
	// }
	clientReq := &v1alpha12.GetClientRequest{
		Name: "clients/" + data.Id.ValueString(),
	}

	clientResp, err := r.client.GetClient(ctx, clientReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Fetching Client",
			err.Error(),
		)
		return
	}

	data.Id = types.StringValue(clientResp.Id)
	data.Name = types.StringValue(clientResp.Name)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ClientResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data v1alpha1.ClientModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update client, got error: %s", err))
	//     return
	// }
	clientReq := &v1alpha12.UpdateClientRequest{
		Name: "clients/" + data.Id.ValueString(),
		Client: &v1alpha13.Client{
			Id:   data.Id.ValueString(),
			Name: data.Name.ValueString(),
		},
	}

	clientResp, err := r.client.UpdateClient(ctx, clientReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Client",
			err.Error(),
		)
		return
	}

	data.Id = types.StringValue(clientResp.Id)
	data.Name = types.StringValue(clientResp.Name)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ClientResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data v1alpha1.ClientModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete client, got error: %s", err))
	//     return
	// }

	clientReq := &v1alpha12.DeleteClientRequest{
		Name: "clients/" + data.Id.ValueString(),
	}

	_, err := r.client.DeleteClient(ctx, clientReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Fetching Client",
			err.Error(),
		)
	}

	return
}

func (r *ClientResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
