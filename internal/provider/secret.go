package provider

import (
	"context"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// secretCollection is the AIP collection a secret's name sits in.
const secretCollection = "secrets"

var secretScope = aip.NewScope("tenants")

var (
	_ resource.Resource                   = &secretResource{}
	_ resource.ResourceWithConfigure      = &secretResource{}
	_ resource.ResourceWithImportState    = &secretResource{}
	_ resource.ResourceWithModifyPlan     = &secretResource{}
	_ resource.ResourceWithValidateConfig = &secretResource{}
)

// secretResource is authwise_secret, hand-written because the material never
// rides the Secret entity (kit#369): it goes in on CreateSecretRequest.payload
// and on each AddSecretVersion, and no RPC returns it.
//
// The material is a write-only argument (Terraform >= 1.11), so it is never
// stored in state or plan. Terraform cannot diff a value it does not keep,
// which is what payload_wo_version is for: changing it is the signal to
// rotate, and the apply sends the configured payload_wo as a new version.
type secretResource struct {
	client   identitypb.AuthwiseIdentityServiceClient
	defaults map[string]string
}

type secretModel struct {
	Name             types.String `tfsdk:"name"`
	TenantID         types.String `tfsdk:"tenant_id"`
	DisplayName      types.String `tfsdk:"display_name"`
	Description      types.String `tfsdk:"description"`
	Labels           types.Map    `tfsdk:"labels"`
	External         types.Object `tfsdk:"external"`
	PayloadWO        types.String `tfsdk:"payload_wo"`
	PayloadWOVersion types.Int64  `tfsdk:"payload_wo_version"`
	Version          types.Int64  `tfsdk:"version"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

type secretExternalModel struct {
	Store types.String `tfsdk:"store"`
	Key   types.String `tfsdk:"key"`
}

func secretExternalAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"store": types.StringType, "key": types.StringType}
}

func newSecretResource() resource.Resource {
	return &secretResource{}
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	optionalComputedString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: desc,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A tenant secret: a credential other objects name by reference (`client_secret_ref`, an endpoint's `auth`) instead of holding it.\n\n" +
			"**Where the material lives.** An inline secret's material goes in through `payload_wo`, a write-only argument (Terraform 1.11 or later): " +
			"kit seals it in the request that carries it, and neither kit nor Terraform keeps a readable copy. It is not in the plan, not in state, and no API call returns it. " +
			"To rotate, change the material and bump `payload_wo_version`; the apply sends it as a new version. Changing `payload_wo` alone does nothing, because Terraform has nothing to compare it with.\n\n" +
			"**Deletion.** kit refuses to delete a secret while anything references it. Reference it by expression (`authwise_secret.x.name`) so Terraform orders the referrer's change or destruction first.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full resource name (`tenants/{t}/secrets/{s}`); serves as the Terraform ID, and is what references take.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tenant_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Parent identifier `tenant_id`; overrides the provider default. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			"display_name": optionalComputedString(""),
			"description":  optionalComputedString(""),
			"labels": schema.MapAttribute{
				Optional:      true,
				Computed:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"external": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Keep the material in an operator-declared store instead of kit's database: kit reads it at use and never holds it. " +
					"Unset means inline. Fixed at creation — changing it replaces the secret. kit refuses external secrets until kit#372 ships.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"store": schema.StringAttribute{Required: true, MarkdownDescription: "A store declared under `secrets.stores` in the install's config."},
					"key":   schema.StringAttribute{Required: true, MarkdownDescription: "The key within that store: a file name for a file store, the variable suffix for an env store."},
				},
			},
			"payload_wo": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				WriteOnly:           true,
				MarkdownDescription: "The material, write-only: required for an inline secret on create, refused for an external one. Sent on create and whenever `payload_wo_version` changes; never stored.",
			},
			"payload_wo_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Change this to send the current `payload_wo` as a new version of the secret (kit's `:addVersion`).",
			},
			"version": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "kit's version counter: 1 at creation, bumped by each rotation.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the secret last changed, RFC 3339.",
			},
		},
	}
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.defaults = pd.Defaults
	}
}

// ValidateConfig refuses the combinations kit would refuse at apply, so they
// fail at plan: material for an external secret, and a version with no
// material to send.
func (r *secretResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var m secretModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !m.External.IsNull() && !m.PayloadWO.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("payload_wo"), "external secrets take no material",
			"kit reads an external secret's material from its store at use; remove payload_wo, or remove external to keep the material in kit.")
	}
	if !m.PayloadWOVersion.IsNull() && m.PayloadWO.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("payload_wo_version"), "payload_wo_version without payload_wo",
			"payload_wo_version exists to send payload_wo again; set the material it should send.")
	}
}

// ModifyPlan marks version and updated_at unknown when a rotation is planned,
// since the apply bumps both.
func (r *secretResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {

	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var planned, prior types.Int64
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("payload_wo_version"), &planned)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("payload_wo_version"), &prior)...)
	if resp.Diagnostics.HasError() || planned.Equal(prior) {
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("version"), types.Int64Unknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("updated_at"), types.StringUnknown())...)
}

func (r *secretResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("secret resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, config secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ids := map[string]string{"tenant_id": r.defaults["tenant_id"]}
	if v := plan.TenantID.ValueString(); v != "" {
		ids["tenant_id"] = v
	}
	parent, err := secretScope.ComposeParent(ids)
	if err != nil {
		resp.Diagnostics.AddError("cannot resolve parent for secret", err.Error()+"; set it on the resource or as a provider default")
		return
	}

	e, diags := plan.toProto(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &identitypb.CreateSecretRequest{Parent: parent, Secret: e}

	// Write-only values reach the provider through config alone.
	if e.GetSource().GetExternal() == nil {
		if config.PayloadWO.IsNull() || config.PayloadWO.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root("payload_wo"), "payload_wo is required",
				"an inline secret is created with its material; set payload_wo (Terraform 1.11 or later), or set external to keep it in a store.")
			return
		}
		createReq.Payload = textPayload(config.PayloadWO.ValueString())
	}

	out, err := r.client.CreateSecret(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("create secret failed", err.Error())
		return
	}

	resp.Diagnostics.Append(plan.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.GetSecret(ctx, &identitypb.GetSecretRequest{Name: state.Name.ValueString()})
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("read secret failed", err.Error())
		return
	}

	resp.Diagnostics.Append(state.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update patches the metadata that changed, then rotates when
// payload_wo_version moved. The two are separate RPCs: kit keeps the
// material off the entity, so no patch can carry it.
func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state, config secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()

	var out *corepb.Secret

	if mask := plan.updateMask(&state); len(mask) > 0 {
		e, diags := plan.toProto(ctx)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		out, err = r.client.PatchSecret(ctx, &identitypb.PatchSecretRequest{
			Name:       name,
			Secret:     e,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: mask},
		})
		if err != nil {
			resp.Diagnostics.AddError("update secret failed", err.Error())
			return
		}
	}

	if !plan.PayloadWOVersion.Equal(state.PayloadWOVersion) {
		if out = r.rotate(ctx, name, config.PayloadWO, &resp.Diagnostics); out == nil {
			return
		}
	}

	if out == nil {
		var err error
		if out, err = r.client.GetSecret(ctx, &identitypb.GetSecretRequest{Name: name}); err != nil {
			resp.Diagnostics.AddError("read secret failed", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(plan.fromProto(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// rotate sends the configured material as a new version; nil means it
// failed and diags says why.
func (r *secretResource) rotate(ctx context.Context, name string, payload types.String, diags *diag.Diagnostics) *corepb.Secret {

	if payload.IsNull() || payload.IsUnknown() {
		diags.AddAttributeError(path.Root("payload_wo"), "payload_wo is required to rotate",
			"payload_wo_version changed, which sends payload_wo as a new version; set it.")
		return nil
	}

	out, err := r.client.AddSecretVersion(ctx, &identitypb.AddSecretVersionRequest{
		Name:    name,
		Payload: textPayload(payload.ValueString()),
	})
	if err != nil {
		diags.AddError("rotating the secret failed", err.Error())
		return nil
	}

	return out
}

// updateMask returns the patchable metadata paths that differ from prior.
func (m *secretModel) updateMask(prior *secretModel) []string {

	var mask []string
	if !m.DisplayName.Equal(prior.DisplayName) {
		mask = append(mask, "display_name")
	}
	if !m.Description.Equal(prior.Description) {
		mask = append(mask, "description")
	}
	if !m.Labels.Equal(prior.Labels) {
		mask = append(mask, "labels")
	}

	return mask
}

// Delete surfaces kit's refusal to delete a referenced secret with the fix
// spelled out; NotFound counts as already deleted.
func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.DeleteSecret(ctx, &identitypb.DeleteSecretRequest{Name: state.Name.ValueString()})
	switch {
	case err == nil, aip.IsNotFound(err):
	case status.Code(err) == codes.FailedPrecondition:
		resp.Diagnostics.AddError("the secret is still referenced",
			err.Error()+"\n\nkit refuses to delete a secret while a provider, endpoint or factor references it. "+
				"Repoint or remove the reference first — referencing the secret by expression (authwise_secret.x.name) makes Terraform do that in the right order.")
	default:
		resp.Diagnostics.AddError("delete secret failed", err.Error())
	}
}

// ImportState takes the full resource name. The material cannot be
// imported — nothing returns it — so an imported inline secret keeps its
// current material until payload_wo_version is next set or changed.
func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := secretScope.ParseName(secretCollection, req.ID); err != nil {
		resp.Diagnostics.AddError("invalid import ID for secret", err.Error())
		return
	}

	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func textPayload(s string) *corepb.SecretPayload {
	return &corepb.SecretPayload{Data: &corepb.SecretPayload_Text{Text: s}}
}

// toProto builds the entity from a plan. The material is never part of it.
func (m *secretModel) toProto(ctx context.Context) (*corepb.Secret, diag.Diagnostics) {

	var diags diag.Diagnostics

	e := &corepb.Secret{
		DisplayName: m.DisplayName.ValueString(),
		Description: m.Description.ValueString(),
		Source:      &corepb.SecretSource{Kind: &corepb.SecretSource_Inline{Inline: &corepb.SecretSourceInline{}}},
	}

	if !m.Labels.IsNull() && !m.Labels.IsUnknown() {
		diags.Append(m.Labels.ElementsAs(ctx, &e.Labels, false)...)
	}

	if !m.External.IsNull() && !m.External.IsUnknown() {
		var ext secretExternalModel
		diags.Append(m.External.As(ctx, &ext, basetypes.ObjectAsOptions{})...)
		e.Source = &corepb.SecretSource{Kind: &corepb.SecretSource_External{External: &corepb.SecretSourceExternal{
			Store: ext.Store.ValueString(),
			Key:   ext.Key.ValueString(),
		}}}
	}

	return e, diags
}

// fromProto reads the server's entity into the model, leaving the
// practitioner-owned inputs (tenant_id, payload_wo_version) alone and the
// write-only material null.
func (m *secretModel) fromProto(ctx context.Context, e *corepb.Secret) diag.Diagnostics {

	var diags diag.Diagnostics

	m.Name = types.StringValue(e.GetName())
	m.DisplayName = nullableString(e.GetDisplayName())
	m.Description = nullableString(e.GetDescription())
	m.PayloadWO = types.StringNull()
	m.Version = types.Int64Value(int64(e.GetVersion()))

	m.UpdatedAt = types.StringNull()
	if ts := e.GetUpdatedAt(); ts != nil {
		m.UpdatedAt = types.StringValue(ts.AsTime().UTC().Format(time.RFC3339))
	}

	m.Labels = types.MapNull(types.StringType)
	if len(e.GetLabels()) > 0 {
		var d diag.Diagnostics
		m.Labels, d = types.MapValueFrom(ctx, types.StringType, e.GetLabels())
		diags.Append(d...)
	}

	m.External = types.ObjectNull(secretExternalAttrTypes())
	if ext := e.GetSource().GetExternal(); ext != nil {
		var d diag.Diagnostics
		m.External, d = types.ObjectValueFrom(ctx, secretExternalAttrTypes(), secretExternalModel{
			Store: types.StringValue(ext.GetStore()),
			Key:   types.StringValue(ext.GetKey()),
		})
		diags.Append(d...)
	}

	if m.TenantID.IsUnknown() {
		m.TenantID = types.StringNull()
	}

	return diags
}

func nullableString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// --- data source ---

var (
	_ datasource.DataSource              = &secretDataSource{}
	_ datasource.DataSourceWithConfigure = &secretDataSource{}
)

// secretDataSource reads a secret's metadata by full name. There is no
// material to read: no RPC returns it.
type secretDataSource struct {
	client identitypb.AuthwiseIdentityServiceClient
}

type secretDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	DisplayName types.String `tfsdk:"display_name"`
	Description types.String `tfsdk:"description"`
	Labels      types.Map    `tfsdk:"labels"`
	External    types.Object `tfsdk:"external"`
	Version     types.Int64  `tfsdk:"version"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func newSecretDataSource() datasource.DataSource {
	return &secretDataSource{}
}

func (d *secretDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (d *secretDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		MarkdownDescription: "Reads a secret's metadata by its full resource name — for referencing a secret managed elsewhere. The material is never readable.",
		Attributes: map[string]dsschema.Attribute{
			"name":         dsschema.StringAttribute{Required: true, MarkdownDescription: "Full resource name of the secret to read."},
			"display_name": dsschema.StringAttribute{Computed: true},
			"description":  dsschema.StringAttribute{Computed: true},
			"labels":       dsschema.MapAttribute{Computed: true, ElementType: types.StringType},
			"external": dsschema.SingleNestedAttribute{
				Computed: true,
				Attributes: map[string]dsschema.Attribute{
					"store": dsschema.StringAttribute{Computed: true},
					"key":   dsschema.StringAttribute{Computed: true},
				},
			},
			"version":    dsschema.Int64Attribute{Computed: true},
			"updated_at": dsschema.StringAttribute{Computed: true},
		},
	}
}

func (d *secretDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *secretDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if d.client == nil {
		resp.Diagnostics.AddError("secret data source not configured", "Configure was not called with tf.ProviderData")
		return
	}

	var cfg secretDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := d.client.GetSecret(ctx, &identitypb.GetSecretRequest{Name: cfg.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("read secret failed", err.Error())
		return
	}

	var m secretModel
	resp.Diagnostics.Append(m.fromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &secretDataSourceModel{
		Name:        m.Name,
		DisplayName: m.DisplayName,
		Description: m.Description,
		Labels:      m.Labels,
		External:    m.External,
		Version:     m.Version,
		UpdatedAt:   m.UpdatedAt,
	})...)
}
