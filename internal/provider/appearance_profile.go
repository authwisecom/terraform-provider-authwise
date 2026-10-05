package provider

import (
	"context"
	"fmt"
	"slices"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// An issuer's appearance is its default appearance profile (kit#680, apis
// v0.18.0). kit holds the invariant: once an issuer has a profile, exactly
// one is its default. The first profile created becomes it whatever
// is_default says. Setting the flag on a second profile, clearing it through
// a mask, and deleting the default while others remain are all
// FAILED_PRECONDITION. The default moves only through
// MakeDefaultAppearanceProfile.
//
// So in configuration is_default takes only true, on the profile that should
// be the default; the others leave it out and read back what kit says. Moving
// the default is setting it on the new profile and removing it from the old
// one, in one apply.

// appearanceProfileTypeName is the Terraform type the wrapper substitutes for.
const appearanceProfileTypeName = "authwise_appearance_profile"

const isDefaultAttr = "is_default"

// defaultDeleteWait is how long deleting an issuer's default profile waits
// for the issuer's other profiles to go first. When a whole issuer's
// profiles are destroyed together, Terraform deletes them in parallel and in
// no particular order, and kit refuses the default until it is the last.
const defaultDeleteWait = time.Minute

// appearanceProfileResource is the generated resource with is_default
// reshaped: true only, and no UseStateForUnknown. Another profile taking the
// default changes this one's flag, so an update of this profile plans it as
// unknown rather than as a stale true.
type appearanceProfileResource struct {
	inner resource.Resource
}

func newAppearanceProfileResource() resource.Resource {
	return &appearanceProfileResource{inner: generated.NewAppearanceProfileResource()}
}

func (r *appearanceProfileResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *appearanceProfileResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	attr, ok := resp.Schema.Attributes[isDefaultAttr].(schema.BoolAttribute)
	if !ok {
		resp.Diagnostics.AddError("schema", "the generated appearance profile has no bool is_default")
		return
	}
	attr.PlanModifiers = nil
	attr.Validators = append(attr.Validators, onlyTrue{})
	attr.MarkdownDescription = "Whether this profile is its issuer's default, and so the issuer's appearance. " +
		"Set it to `true` on the one profile that should be the default, and leave it out on the others. " +
		"The issuer's first profile becomes the default whatever this says. " +
		"To move the default, set it on the new profile and remove it from the old one."
	resp.Schema.Attributes[isDefaultAttr] = attr
}

func (r *appearanceProfileResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *appearanceProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *appearanceProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *appearanceProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *appearanceProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *appearanceProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

// onlyTrue refuses is_default = false. kit cannot honour it on the issuer's
// first profile, nor on the default until another takes over, so a false
// would plan a value the apply cannot produce.
type onlyTrue struct{}

func (onlyTrue) Description(context.Context) string {
	return "must be true when set"
}

func (v onlyTrue) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (onlyTrue) ValidateBool(_ context.Context, req validator.BoolRequest, resp *validator.BoolResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.ValueBool() {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "is_default can only be true",
		"An issuer always has exactly one default profile, and kit decides which until you choose: "+
			"its first profile, until another is made the default. Leave is_default out on this profile, "+
			"and set is_default = true on the profile that should be the default.")
}

// appearanceDefaultClient routes is_default through
// MakeDefaultAppearanceProfile and never writes the flag itself. The
// generated resource's writes copy state into the body, so a write would
// otherwise echo a stale flag that kit refuses (the kit#662 trap).
type appearanceDefaultClient struct {
	identitypb.AuthwiseIdentityServiceClient
	// deleteWait bounds how long deleting the default waits for its
	// siblings; poll is the interval between attempts.
	deleteWait, poll time.Duration
}

func newAppearanceDefaultClient(inner identitypb.AuthwiseIdentityServiceClient) appearanceDefaultClient {
	return appearanceDefaultClient{AuthwiseIdentityServiceClient: inner, deleteWait: defaultDeleteWait, poll: 500 * time.Millisecond}
}

// makeDefault makes p the default unless it already is.
func (c appearanceDefaultClient) makeDefault(ctx context.Context, p *corepb.AppearanceProfile, opts ...grpc.CallOption) (*corepb.AppearanceProfile, error) {
	if p.GetIsDefault() {
		return p, nil
	}
	out, err := c.MakeDefaultAppearanceProfile(ctx,
		&identitypb.MakeDefaultAppearanceProfileRequest{Name: p.GetName()}, opts...)
	if err != nil {
		return nil, err
	}
	return out.GetDefault(), nil
}

func (c appearanceDefaultClient) CreateAppearanceProfile(ctx context.Context, in *identitypb.CreateAppearanceProfileRequest, opts ...grpc.CallOption) (*corepb.AppearanceProfile, error) {
	want := in.GetAppearanceProfile().GetIsDefault()
	if in.GetAppearanceProfile() != nil {
		in.AppearanceProfile.IsDefault = false
	}
	created, err := c.AuthwiseIdentityServiceClient.CreateAppearanceProfile(ctx, in, opts...)
	if err != nil || !want {
		return created, err
	}
	out, err := c.makeDefault(ctx, created, opts...)
	if err != nil {
		// Not the default, so kit lets it go; leaving it would orphan a
		// row Terraform never recorded.
		_, _ = c.AuthwiseIdentityServiceClient.DeleteAppearanceProfile(ctx,
			&identitypb.DeleteAppearanceProfileRequest{Name: created.GetName()}, opts...)
		return nil, fmt.Errorf("making %s the default: %w", created.GetName(), err)
	}
	return out, nil
}

func (c appearanceDefaultClient) PatchAppearanceProfile(ctx context.Context, in *identitypb.PatchAppearanceProfileRequest, opts ...grpc.CallOption) (*corepb.AppearanceProfile, error) {
	want := in.GetAppearanceProfile().GetIsDefault()
	if in.GetAppearanceProfile() != nil {
		in.AppearanceProfile.IsDefault = false
	}
	paths := in.GetUpdateMask().GetPaths()
	if !slices.Contains(paths, isDefaultAttr) {
		return c.AuthwiseIdentityServiceClient.PatchAppearanceProfile(ctx, in, opts...)
	}
	// A false here is an unknown plan, never a configured false (onlyTrue):
	// the flag is kit's to report, not to clear.
	rest := slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return p == isDefaultAttr })
	var (
		out *corepb.AppearanceProfile
		err error
	)
	if len(rest) > 0 {
		in.UpdateMask = &fieldmaskpb.FieldMask{Paths: rest}
		out, err = c.AuthwiseIdentityServiceClient.PatchAppearanceProfile(ctx, in, opts...)
	} else {
		// An empty mask is a full update in AIP; read instead.
		out, err = c.GetAppearanceProfile(ctx,
			&identitypb.GetAppearanceProfileRequest{Name: in.GetName()}, opts...)
	}
	if err != nil || !want {
		return out, err
	}
	return c.makeDefault(ctx, out, opts...)
}

func (c appearanceDefaultClient) UpdateAppearanceProfile(ctx context.Context, in *identitypb.UpdateAppearanceProfileRequest, opts ...grpc.CallOption) (*corepb.AppearanceProfile, error) {
	want := in.GetAppearanceProfile().GetIsDefault()
	if in.GetAppearanceProfile() != nil {
		in.AppearanceProfile.IsDefault = false
	}
	out, err := c.AuthwiseIdentityServiceClient.UpdateAppearanceProfile(ctx, in, opts...)
	if err != nil || !want {
		return out, err
	}
	return c.makeDefault(ctx, out, opts...)
}

// DeleteAppearanceProfile waits out kit's refusal to delete the default
// while the issuer has other profiles, for as long as deleteWait: in a
// destroy of them all, the others are going at the same time.
func (c appearanceDefaultClient) DeleteAppearanceProfile(ctx context.Context, in *identitypb.DeleteAppearanceProfileRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	deadline := time.Now().Add(c.deleteWait)
	for {
		out, err := c.AuthwiseIdentityServiceClient.DeleteAppearanceProfile(ctx, in, opts...)
		if status.Code(err) != codes.FailedPrecondition {
			return out, err
		}
		p, getErr := c.GetAppearanceProfile(ctx,
			&identitypb.GetAppearanceProfileRequest{Name: in.GetName()}, opts...)
		if getErr != nil || !p.GetIsDefault() {
			return out, err
		}
		if time.Now().After(deadline) {
			return out, fmt.Errorf("%w; it is the issuer's default, and the issuer still has other profiles after %s. "+
				"Set is_default = true on the profile that should take over, or destroy the others with it", err, c.deleteWait)
		}
		select {
		case <-ctx.Done():
			return out, err
		case <-time.After(c.poll):
		}
	}
}
