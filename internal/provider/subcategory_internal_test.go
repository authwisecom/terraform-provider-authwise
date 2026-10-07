package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
)

// Every resource and data source the provider registers has a Registry
// group (#31), and every Identity entry names a registered type: an
// unmapped new type fails here rather than in a Registry review.
func TestSubcategoryCoversTheProvider(t *testing.T) {

	ctx := context.Background()
	p := &AuthwiseProvider{}
	registered := map[string]bool{}

	for _, f := range p.Resources(ctx) {
		m := &resource.MetadataResponse{}
		f().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, m)
		registered[m.TypeName] = true
	}
	for _, f := range p.DataSources(ctx) {
		m := &datasource.MetadataResponse{}
		f().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, m)
		registered[m.TypeName] = true
	}

	for typeName := range registered {
		assert.NotEmpty(t, Subcategory(typeName), "%s has no subcategory", typeName)
		if strings.HasPrefix(typeName, guardTypePrefix) {
			assert.Equal(t, SubcategoryGuard, Subcategory(typeName))
		}
	}
	for name := range identityTypes {
		assert.True(t, registered[providerTypeName+"_"+name], "identityTypes lists %s, which the provider does not register", name)
	}
}
