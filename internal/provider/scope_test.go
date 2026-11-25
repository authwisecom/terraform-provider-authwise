package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// TestScopeDescriptions pins what tf#84 says a client gets: the kinds that
// fill the permissions claim, auto, and the association's single effective
// kind.
func TestScopeDescriptions(t *testing.T) {
	ctx := context.Background()

	scope := &resource.SchemaResponse{}
	newScopeResource().Schema(ctx, resource.SchemaRequest{}, scope)
	kind := scope.Schema.Attributes["kind"].GetMarkdownDescription()
	for _, want := range []string{"Only a scope named `roles`", "Only a scope named `permissions`", "groups mode"} {
		if !strings.Contains(kind, want) {
			t.Errorf("kind description lacks %q", want)
		}
	}
	if scope.Schema.Attributes["auto"].GetMarkdownDescription() != scopeAutoDescription {
		t.Error("auto is not described")
	}

	assoc := &resource.SchemaResponse{}
	newScopeAccessPermissionsResource().Schema(ctx, resource.SchemaRequest{}, assoc)
	if !strings.HasSuffix(assoc.Schema.MarkdownDescription, scopeAccessPermissionsNote) {
		t.Error("scope_access_permissions does not say it acts only on authorization.permissionGroups")
	}

	meta := &resource.MetadataResponse{}
	newScopeAccessPermissionsResource().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, meta)
	if meta.TypeName != scopeAccessPermissionsTypeName {
		t.Errorf("wrapper reports %q, want %q", meta.TypeName, scopeAccessPermissionsTypeName)
	}
}
