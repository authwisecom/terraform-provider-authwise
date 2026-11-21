package provider

import (
	"context"
	"strings"
	"testing"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// The six settings tf#82 names carry a description on the resource and on
// both data sources, the list's nested attributes included.
var signInProviderDescribed = []string{
	"trust_upstream_email_verified", "trust_upstream_amr", "upstream_acr_map",
	"link_by_verified_email", "status", "provider_type",
}

func TestSignInProviderResourceDescribesItsSettings(t *testing.T) {
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	newSignInProviderResource().Schema(ctx, resource.SchemaRequest{}, resp)
	for _, name := range signInProviderDescribed {
		a, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Fatalf("authwise_provider has no %s", name)
		}
		if a.GetMarkdownDescription() == "" {
			t.Errorf("authwise_provider.%s has no description", name)
		}
	}
	// A limit is appended to its description, not shown in its place.
	got := resp.Schema.Attributes["trust_upstream_amr"].GetMarkdownDescription()
	if !strings.HasPrefix(got, "Whether the sign-in methods") || !strings.HasSuffix(got, "(kit#1010).") {
		t.Errorf("trust_upstream_amr description = %q", got)
	}
}

func TestSignInProviderDataSourcesDescribeTheirSettings(t *testing.T) {
	ctx := context.Background()
	wrapped := describeSignInProviderDataSources(ctx, []func() datasource.DataSource{
		generated.NewProviderDataSource, generated.NewProvidersDataSource,
	})

	single := &datasource.SchemaResponse{}
	wrapped[0]().Schema(ctx, datasource.SchemaRequest{}, single)
	list := &datasource.SchemaResponse{}
	wrapped[1]().Schema(ctx, datasource.SchemaRequest{}, list)
	nested := list.Schema.Attributes[signInProvidersListAttr].(dsschema.ListNestedAttribute).NestedObject.Attributes

	for _, name := range signInProviderDescribed {
		if single.Schema.Attributes[name].GetMarkdownDescription() == "" {
			t.Errorf("data authwise_provider.%s has no description", name)
		}
		if nested[name].GetMarkdownDescription() == "" {
			t.Errorf("data authwise_providers.providers.%s has no description", name)
		}
	}
}

// Every limit belongs to a described setting, so none is silently dropped.
func TestSignInProviderLimitsNameDescribedSettings(t *testing.T) {
	for name := range signInProviderLimits {
		if signInProviderDescriptions[name] == "" {
			t.Errorf("limit for %s, which has no description", name)
		}
	}
}
