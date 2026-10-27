package acctest_test

import (
	"context"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Themes are tenant-scoped (#50). As in kit, an empty layout on create
// means center, and the layout is fixed at creation (kit#564).

func (f *fakeIdentityServer) GetTheme(ctx context.Context, in *identitypb.GetThemeRequest) (*corepb.Theme, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	th, ok := f.themes[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "theme %q not found", in.GetName())
	}
	return proto.Clone(th).(*corepb.Theme), nil
}

func (f *fakeIdentityServer) CreateTheme(ctx context.Context, in *identitypb.CreateThemeRequest) (*corepb.Theme, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	th := proto.Clone(in.GetTheme()).(*corepb.Theme)
	if th.GetDisplayName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "display_name is required")
	}
	if th.GetLayout() == "" {
		th.Layout = "center"
	}
	th.Name = in.GetParent() + "/themes/" + f.nextID("th")
	f.themes[th.GetName()] = th
	return proto.Clone(th).(*corepb.Theme), nil
}

func (f *fakeIdentityServer) PatchTheme(ctx context.Context, in *identitypb.PatchThemeRequest) (*corepb.Theme, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.themes[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "theme %q not found", in.GetName())
	}
	t := in.GetTheme()
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "labels":
			existing.Labels = t.GetLabels()
		case "display_name":
			existing.DisplayName = t.GetDisplayName()
		case "stylesheet":
			existing.Stylesheet = t.GetStylesheet()
		case "stylesheet_attributes":
			existing.StylesheetAttributes = t.GetStylesheetAttributes()
		case "stylesheet_attributes_schema":
			existing.StylesheetAttributesSchema = t.GetStylesheetAttributesSchema()
		case "content":
			existing.Content = t.GetContent()
		case "content_schema":
			existing.ContentSchema = t.GetContentSchema()
		case "placeholder_stylesheet_attributes":
			existing.PlaceholderStylesheetAttributes = t.GetPlaceholderStylesheetAttributes()
		case "placeholder_content":
			existing.PlaceholderContent = t.GetPlaceholderContent()
		case "layout":
			if t.GetLayout() != existing.GetLayout() {
				return nil, status.Errorf(codes.InvalidArgument,
					"a theme's layout is fixed at creation: this theme is %q and cannot become %q; create a new theme for the other layout",
					existing.GetLayout(), t.GetLayout())
			}
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Theme), nil
}

func (f *fakeIdentityServer) DeleteTheme(ctx context.Context, in *identitypb.DeleteThemeRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.themes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "theme %q not found", in.GetName())
	}
	delete(f.themes, in.GetName())
	return &emptypb.Empty{}, nil
}
