package provider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// assetCollection is the AIP collection an asset's name sits in.
const assetCollection = "assets"

// assetChunkSize is the size of each UploadAssetRequest. It is well under
// gRPC's 4 MiB default message limit on either side.
const assetChunkSize = 64 << 10

var assetScope = aip.NewScope("tenants")

var (
	_ resource.Resource                = &assetContentResource{}
	_ resource.ResourceWithConfigure   = &assetContentResource{}
	_ resource.ResourceWithImportState = &assetContentResource{}
	_ resource.ResourceWithModifyPlan  = &assetContentResource{}
)

// assetContentResource is authwise_asset_content: the bytes behind an
// authwise_asset. kit keeps the blob apart from the Asset row. It goes in
// through the UploadAsset client stream and comes back through DownloadAsset,
// and neither is part of the generated CRUD, so the generated authwise_asset
// only ever manages the row.
//
// The content is diffed by its SHA-256. ModifyPlan hashes the local bytes, so
// an edited file plans as an in-place update. Read hashes what kit serves, so
// a change made outside Terraform shows as drift.
type assetContentResource struct {
	client   identitypb.AuthwiseIdentityServiceClient
	defaults map[string]string
}

type assetContentModel struct {
	TenantID      types.String `tfsdk:"tenant_id"`
	AssetID       types.String `tfsdk:"asset_id"`
	Source        types.String `tfsdk:"source"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	ContentSHA256 types.String `tfsdk:"content_sha256"`
}

func newAssetContentResource() resource.Resource {
	return &assetContentResource{}
}

func (r *assetContentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_asset_content"
}

func (r *assetContentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	oneOf := stringvalidator.ExactlyOneOf(path.MatchRoot("source"), path.MatchRoot("content_base64"))

	resp.Schema = schema.Schema{
		MarkdownDescription: "The file behind an `authwise_asset`: the bytes the hosted pages serve at the asset's `path`.\n\n" +
			"`authwise_asset` manages only the asset's metadata. kit stores the file separately, through its upload and download calls, and this resource manages that part. " +
			"Set exactly one of `source`, a local file, or `content_base64`. Changes are tracked by `content_sha256`: editing the file plans an in-place update that uploads it again, " +
			"and a file changed outside Terraform shows as drift.\n\n" +
			"Destroying it removes the file and leaves the asset. Destroying the `authwise_asset` removes both.",
		Attributes: map[string]schema.Attribute{
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Parent identifier `tenant_id`; overrides the provider default. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			"asset_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The asset the content belongs to: `authwise_asset.x.asset_id`. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{tfruntime.ReferenceID("as", "asset", "")},
			},
			"source": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to a local file to upload. Conflicts with `content_base64`.",
				Validators:          []validator.String{oneOf},
			},
			"content_base64": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The content, base64-encoded, e.g. `filebase64(\"logo.png\")` or `base64encode(templatefile(...))`. Conflicts with `source`.",
				Validators:          []validator.String{oneOf},
			},
			"content_sha256": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Hex SHA-256 of the content: the planned content before apply, and the content kit serves after a refresh.",
			},
		},
	}
}

func (r *assetContentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientFrom(req.ProviderData, &resp.Diagnostics)
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.defaults = pd.Defaults
	}
}

func (r *assetContentResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("asset_content resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

// ModifyPlan puts the hash of the configured content into the plan. It is
// the only attribute that tracks the bytes: a source path that stays the
// same while the file changes would otherwise plan nothing.
func (r *assetContentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {

	if req.Plan.Raw.IsNull() {
		return
	}

	var plan assetContentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hash := types.StringUnknown()
	if !plan.Source.IsUnknown() && !plan.ContentBase64.IsUnknown() {
		content, err := plan.content()
		if err != nil {
			resp.Diagnostics.AddError("cannot read the asset content", err.Error())
			return
		}
		hash = types.StringValue(sha256Hex(content))
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("content_sha256"), hash)...)
}

func (r *assetContentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *assetContentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// write uploads the planned content and records it as state.
func (r *assetContentResource) write(ctx context.Context, p tfsdk.Plan, state *tfsdk.State, diags *diag.Diagnostics) {

	if !r.configured(diags) {
		return
	}

	var plan assetContentModel
	diags.Append(p.Get(ctx, &plan)...)
	if diags.HasError() {
		return
	}

	name, err := r.assetName(plan)
	if err != nil {
		diags.AddError("cannot resolve the asset", err.Error())
		return
	}

	content, err := plan.content()
	if err != nil {
		diags.AddError("cannot read the asset content", err.Error())
		return
	}

	if err := r.upload(ctx, name, content); err != nil {
		diags.AddError("uploading the asset content failed", err.Error())
		return
	}

	plan.ContentSHA256 = types.StringValue(sha256Hex(content))
	diags.Append(state.Set(ctx, &plan)...)
}

// Read hashes the content kit serves. A missing asset, or an asset without
// content, drops the resource from state so the next plan uploads again.
func (r *assetContentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state assetContentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := r.assetName(state)
	if err != nil {
		resp.Diagnostics.AddError("cannot resolve the asset", err.Error())
		return
	}

	hash, err := r.downloadHash(ctx, name)
	if err != nil {
		if aip.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the asset content failed", err.Error())
		return
	}

	state.ContentSHA256 = types.StringValue(hash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete removes the file and leaves the asset row. A file or asset that is
// already gone is success.
func (r *assetContentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state assetContentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := r.assetName(state)
	if err != nil {
		resp.Diagnostics.AddError("cannot resolve the asset", err.Error())
		return
	}

	_, err = r.client.RemoveAsset(ctx, &identitypb.RemoveAssetRequest{Name: name})
	if err != nil && !aip.IsNotFound(err) {
		resp.Diagnostics.AddError("removing the asset content failed", err.Error())
	}
}

// ImportState takes the asset's full resource name (tenants/{t}/assets/{a}).
// The refresh that follows reads the content's hash. source or
// content_base64 then comes from configuration, and the first apply uploads
// it. tenant_id is left null when it is the provider default, as a
// configuration relying on the default leaves it; setting it would plan a
// replacement.
func (r *assetContentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	ids, id, err := assetScope.ParseName(assetCollection, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", "expected an asset name, tenants/{tenant_id}/assets/{asset_id}: "+err.Error())
		return
	}

	if t := ids[tenantIDAttribute]; t != r.defaults[tenantIDAttribute] {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), t)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("asset_id"), id)...)
}

// assetName composes the asset's full name from its id and tenant, falling
// back to the provider's tenant default.
func (r *assetContentResource) assetName(m assetContentModel) (string, error) {
	ids := map[string]string{tenantIDAttribute: r.defaults[tenantIDAttribute]}
	if v := m.TenantID.ValueString(); v != "" {
		ids[tenantIDAttribute] = v
	}
	name, err := assetScope.ComposeName(assetCollection, ids, m.AssetID.ValueString())
	if err != nil {
		return "", fmt.Errorf("%w; set tenant_id on the resource or as a provider default", err)
	}
	return name, nil
}

// upload streams content to kit in chunks, each carrying the asset's name.
// Empty content still sends one message, since kit takes the name from it.
func (r *assetContentResource) upload(ctx context.Context, name string, content []byte) error {

	stream, err := r.client.UploadAsset(ctx)
	if err != nil {
		return err
	}

	for off := 0; off == 0 || off < len(content); off += assetChunkSize {
		end := min(off+assetChunkSize, len(content))
		if err := stream.Send(&identitypb.UploadAssetRequest{Name: name, Data: content[off:end]}); err != nil {
			// The stream's real error arrives with CloseAndRecv; Send
			// reports only io.EOF.
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}

	_, err = stream.CloseAndRecv()
	return err
}

// downloadHash streams the asset's content from kit and returns its hash.
func (r *assetContentResource) downloadHash(ctx context.Context, name string) (string, error) {

	stream, err := r.client.DownloadAsset(ctx, &identitypb.DownloadAssetRequest{Name: name})
	if err != nil {
		return "", err
	}

	h := sha256.New()
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
		h.Write(msg.GetData())
	}
}

// content returns the configured bytes: the file at source, or the decoded
// content_base64.
func (m assetContentModel) content() ([]byte, error) {
	if !m.Source.IsNull() {
		b, err := os.ReadFile(m.Source.ValueString())
		if err != nil {
			return nil, fmt.Errorf("source: %w", err)
		}
		return b, nil
	}
	b, err := base64.StdEncoding.DecodeString(m.ContentBase64.ValueString())
	if err != nil {
		return nil, fmt.Errorf("content_base64: %w", err)
	}
	return b, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
