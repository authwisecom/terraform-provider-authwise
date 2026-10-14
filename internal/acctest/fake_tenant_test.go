package acctest_test

import (
	"context"
	"strings"

	tenancypb "git.authwise.com/authwise/apis/authwise/tenancy/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The tenant as the public tenancy service writes it (kit#166, kit#638):
// PatchTenant merges display_name, labels and config under the mask and
// refuses any other path; the merged brand must name image assets of the
// tenant, and a dark logo needs a light one. kit refuses deleting or
// emptying an asset while a tenant's brand names it; the fake identity
// server asks brandHolding before either.

// tenantMutablePaths are the paths the public PatchTenant writes.
var tenantMutablePaths = map[string]bool{"display_name": true, "labels": true, "config": true}

// seedTenant is the tenant the harness's provider defaults name, as
// awtenant would have made it.
func seedTenant() *corepb.Tenant {
	return &corepb.Tenant{
		Name:           "tenants/t-1",
		DisplayName:    "Acceptance",
		Classification: corepb.Classification_CLASSIFICATION_WORKPLACE,
		Owner:          "org-1",
	}
}

func (f *fakeTenancyServer) GetTenant(_ context.Context, in *tenancypb.GetTenantRequest) (*corepb.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", in.GetName())
	}
	return proto.Clone(t).(*corepb.Tenant), nil
}

func (f *fakeTenancyServer) PatchTenant(_ context.Context, in *tenancypb.PatchTenantRequest) (*corepb.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found", in.GetName())
	}
	if in.GetTenant() == nil {
		return nil, status.Error(codes.InvalidArgument, "tenant body is required")
	}
	merged := proto.Clone(t).(*corepb.Tenant)
	for _, p := range in.GetUpdateMask().GetPaths() {
		if !tenantMutablePaths[p] {
			return nil, status.Errorf(codes.InvalidArgument,
				"update_mask path %q: PatchTenant writes only display_name, labels, config", p)
		}
		switch p {
		case "display_name":
			merged.DisplayName = in.GetTenant().GetDisplayName()
		case "labels":
			merged.Labels = in.GetTenant().GetLabels()
		case "config":
			merged.Config = in.GetTenant().GetConfig()
		}
	}
	if err := f.checkBrand(in.GetName(), merged.GetConfig().GetBranding()); err != nil {
		return nil, err
	}
	f.tenants[in.GetName()] = merged
	return proto.Clone(merged).(*corepb.Tenant), nil
}

// checkBrand is kit's branding.Check: a dark logo needs a light one, and
// each names an image asset of the tenant.
func (f *fakeTenancyServer) checkBrand(tenant string, b *corepb.Branding) error {
	light, dark := b.GetLogoAssetId(), b.GetLogoDarkAssetId()
	if dark != "" && light == "" {
		return status.Error(codes.InvalidArgument,
			"config.branding.logo_dark_asset_id: set without logo_asset_id; a dark variant needs the logo it varies")
	}
	for _, ref := range []struct{ field, id string }{{"logo_asset_id", light}, {"logo_dark_asset_id", dark}} {
		if ref.id == "" {
			continue
		}
		mime, ok := f.identity.assetMime(tenant + "/assets/" + ref.id)
		if !ok || strings.Contains(ref.id, "/") {
			return status.Errorf(codes.InvalidArgument, "config.branding.%s: no asset %s in tenant", ref.field, ref.id)
		}
		if !strings.HasPrefix(mime, "image/") {
			return status.Errorf(codes.InvalidArgument, "config.branding.%s: asset %s is %s, not an image", ref.field, ref.id, mime)
		}
	}
	return nil
}

// brandHolding is kit's brandReferrer: FailedPrecondition when the
// tenant's brand names the asset. Called without the identity lock held.
func (f *fakeTenancyServer) brandHolding(asset string) error {
	tenant, id, ok := strings.Cut(asset, "/assets/")
	if !ok {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	br := f.tenants[tenant].GetConfig().GetBranding()
	for _, ref := range []struct{ field, id string }{{"logo_asset_id", br.GetLogoAssetId()}, {"logo_dark_asset_id", br.GetLogoDarkAssetId()}} {
		if ref.id == id {
			return status.Errorf(codes.FailedPrecondition,
				"asset %s is the logo of %s (config.branding.%s); change the tenant's branding before removing it", id, tenant, ref.field)
		}
	}
	return nil
}

// tenant returns a stored tenant.
func (f *fakeTenancyServer) tenant(name string) *corepb.Tenant {
	f.mu.Lock()
	defer f.mu.Unlock()
	return proto.Clone(f.tenants[name]).(*corepb.Tenant)
}

// editTenant changes a tenant out of band, as the console would.
func (f *fakeTenancyServer) editTenant(name string, edit func(*corepb.Tenant)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	edit(f.tenants[name])
}

// assetMime is an asset's mime type, and whether it exists.
func (f *fakeIdentityServer) assetMime(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.assets[name]
	return a.GetMimeType(), ok
}

// brandRefusal is the fake tenancy server's brandHolding, or nil before
// the harness wires it.
func (f *fakeIdentityServer) brandRefusal(asset string) error {
	if f.brandHolding == nil {
		return nil
	}
	return f.brandHolding(asset)
}
