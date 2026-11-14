package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The vocabulary pass (kit#968, VS-D5, VS-D8) renamed three resources with
// no aliases: authwise_client is authwise_application, authwise_client_secret
// is authwise_application_secret and authwise_appearance_profile is
// authwise_theme_variant. A `moved` block from the old type to the new one
// carries the state across: the attributes named for the old noun take the
// new name, and the resource name's collection segment takes kit's new one,
// so the next refresh reads the same row at its new address. Nothing is
// destroyed or recreated, and an application secret keeps its `secret`.

// renamedFrom is how one resource's state reads under its former type.
type renamedFrom struct {
	// typeName is the former Terraform type.
	typeName string
	// attributes maps a former attribute name to its new one.
	attributes map[string]string
	// segments maps a former collection segment of `name` to its new one.
	segments map[string]string
}

var (
	applicationRenamedFrom = renamedFrom{
		typeName:   "authwise_client",
		attributes: map[string]string{"client_id": "application_id", "appearance_profile_id": "theme_variant_id"},
		segments:   map[string]string{"clients": "applications"},
	}
	applicationSecretRenamedFrom = renamedFrom{
		typeName:   "authwise_client_secret",
		attributes: map[string]string{"client_id": "application_id"},
		segments:   map[string]string{"clients": "applications", "client-secrets": "application-secrets"},
	}
	themeVariantRenamedFrom = renamedFrom{
		typeName:   "authwise_appearance_profile",
		attributes: map[string]string{"appearance_profile_id": "theme_variant_id"},
		segments:   map[string]string{"appearance-profiles": "theme-variants"},
	}
)

// applicationTypeName and applicationSecretTypeName are the generated
// resources that take a former type's state.
const (
	applicationTypeName       = "authwise_application"
	applicationSecretTypeName = "authwise_application_secret"
)

// renameState is the former state as JSON, with its attributes and the
// segments of its name renamed. Attributes the new type does not have are
// left for the decode to drop.
func (f renamedFrom) renameState(raw []byte) ([]byte, error) {

	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, err
	}
	for from, to := range f.attributes {
		if v, ok := attrs[from]; ok {
			delete(attrs, from)
			attrs[to] = v
		}
	}
	if v, ok := attrs["name"]; ok {
		var name string
		if json.Unmarshal(v, &name) == nil {
			parts := strings.Split(name, "/")
			for i, p := range parts {
				// Collections sit at odd positions: tenants/t/issuers/i/clients/c.
				if to, ok := f.segments[p]; ok && i%2 == 0 {
					parts[i] = to
				}
			}
			b, err := json.Marshal(strings.Join(parts, "/"))
			if err != nil {
				return nil, err
			}
			attrs["name"] = b
		}
	}
	return json.Marshal(attrs)
}

// mover is the StateMover that takes this provider's former type.
func (f renamedFrom) mover() resource.StateMover {
	return resource.StateMover{
		StateMover: func(ctx context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {
			// Another type, or another provider's: not this mover's, so the
			// framework says no move was found.
			if req.SourceTypeName != f.typeName || !isThisProvider(req.SourceProviderAddress) {
				return
			}
			if req.SourceRawState == nil || len(req.SourceRawState.JSON) == 0 {
				resp.Diagnostics.AddError("cannot move "+f.typeName, "the former state holds no JSON to move")
				return
			}
			b, err := f.renameState(req.SourceRawState.JSON)
			if err != nil {
				resp.Diagnostics.AddError("cannot move "+f.typeName, err.Error())
				return
			}
			v, err := (&tfprotov6.RawState{JSON: b}).UnmarshalWithOpts(
				resp.TargetState.Schema.Type().TerraformType(ctx),
				tfprotov6.UnmarshalOpts{ValueFromJSONOpts: tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true}})
			if err != nil {
				resp.Diagnostics.AddError("cannot move "+f.typeName, err.Error())
				return
			}
			resp.TargetState.Raw = v
		},
	}
}

// isThisProvider reports whether a provider address is this provider's,
// under any registry host or namespace a configuration installed it from.
func isThisProvider(address string) bool {
	return address == "" || strings.HasSuffix(address, "/"+providerTypeName)
}

var (
	_ resource.ResourceWithConfigure   = &movedResource{}
	_ resource.ResourceWithImportState = &movedResource{}
	_ resource.ResourceWithMoveState   = &movedResource{}
)

// movedResource is a resource that also takes its former type's state.
type movedResource struct {
	inner resource.Resource
	from  renamedFrom
}

// moved wraps a constructor so its resource takes from's state.
func moved(newInner func() resource.Resource, from renamedFrom) func() resource.Resource {
	return func() resource.Resource { return &movedResource{inner: newInner(), from: from} }
}

func (r *movedResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *movedResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

func (r *movedResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *movedResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *movedResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *movedResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *movedResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *movedResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

func (r *movedResource) MoveState(context.Context) []resource.StateMover {
	return []resource.StateMover{r.from.mover()}
}
