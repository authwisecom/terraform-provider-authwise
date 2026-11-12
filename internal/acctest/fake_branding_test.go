package acctest_test

import (
	"context"
	"strings"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The branding singleton, tenants/{t}/branding (kit#953, apis v0.65.0):
// a view of the tenant's config.branding, with the same check.

// brandingFields are the paths PatchTenantBranding takes.
var brandingFields = map[string]bool{"display_name": true, "theme_id": true, "logo_asset_id": true, "logo_dark_asset_id": true}

func (f *fakeIdentityServer) GetTenantBranding(ctx context.Context, in *identitypb.GetTenantBrandingRequest) (*corepb.Branding, error) {
	tenant, err := brandingTenant(in.GetName())
	if err != nil {
		return nil, err
	}
	tn, err := f.tenancy.GetTenant(ctx, &tenancypb.GetTenantRequest{Name: tenant})
	if err != nil {
		return nil, err
	}
	out := proto.Clone(tn.GetConfig().GetBranding()).(*corepb.Branding)
	if out == nil {
		out = &corepb.Branding{}
	}
	return out, nil
}

func (f *fakeIdentityServer) UpdateTenantBranding(_ context.Context, in *identitypb.UpdateTenantBrandingRequest) (*corepb.Branding, error) {
	return f.writeBranding(in.GetName(), in.GetBranding(), nil)
}

func (f *fakeIdentityServer) PatchTenantBranding(_ context.Context, in *identitypb.PatchTenantBrandingRequest) (*corepb.Branding, error) {
	return f.writeBranding(in.GetName(), in.GetBranding(), in.GetUpdateMask().GetPaths())
}

// writeBranding writes the brand, the fields paths names or all of them,
// through the tenant's config, checked as PatchTenant checks it.
func (f *fakeIdentityServer) writeBranding(name string, in *corepb.Branding, paths []string) (*corepb.Branding, error) {
	tenant, err := brandingTenant(name)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		paths = []string{"display_name", "theme_id", "logo_asset_id", "logo_dark_asset_id"}
	}
	t := f.tenancy
	t.mu.Lock()
	defer t.mu.Unlock()
	tn, ok := t.tenants[tenant]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", tenant)
	}
	merged := proto.Clone(tn.GetConfig().GetBranding()).(*corepb.Branding)
	if merged == nil {
		merged = &corepb.Branding{}
	}
	for _, p := range paths {
		if !brandingFields[p] {
			return nil, status.Errorf(codes.InvalidArgument, "update_mask path %q: the brand has display_name, theme_id, logo_asset_id, logo_dark_asset_id", p)
		}
		switch p {
		case "display_name":
			merged.DisplayName = in.GetDisplayName()
		case "theme_id":
			merged.ThemeId = in.GetThemeId()
		case "logo_asset_id":
			merged.LogoAssetId = in.GetLogoAssetId()
		case "logo_dark_asset_id":
			merged.LogoDarkAssetId = in.GetLogoDarkAssetId()
		}
	}
	if err := t.checkBrand(tenant, merged); err != nil {
		return nil, err
	}
	if tn.GetConfig() == nil {
		tn.Config = &corepb.TenantConfig{}
	}
	tn.Config.Branding = merged
	return proto.Clone(merged).(*corepb.Branding), nil
}

func brandingTenant(name string) (string, error) {
	tenant, ok := strings.CutSuffix(name, "/branding")
	if !ok || !strings.HasPrefix(tenant, "tenants/") {
		return "", status.Errorf(codes.InvalidArgument, "name %q: expected tenants/{tenant}/branding", name)
	}
	return tenant, nil
}
