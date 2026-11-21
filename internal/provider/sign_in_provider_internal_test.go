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
	"link_by_verified_email", "status", "provider_type", "trusted_email_domains",
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
	if !strings.HasPrefix(got, "Whether the upstream's statement") || !strings.HasSuffix(got, signInProviderKitNotes["trust_upstream_amr"]) {
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

// every kit note belongs to a described setting, so none is silently dropped.
func TestSignInProviderLimitsNameDescribedSettings(t *testing.T) {
	for name := range signInProviderKitNotes {
		if signInProviderDescriptions[name] == "" {
			t.Errorf("limit for %s, which has no description", name)
		}
	}
}

// kit's ParseUpstreamAcrKey forms (kit pkg/lib/model/upstream_acr.go).
func TestUpstreamAcrKeyProblem(t *testing.T) {
	for key, ok := range map[string]bool{
		"urn:authwise:acr:2":         true,
		"http://example.com/acr/mfa": true,
		entraAcrKey:                  true,
		"attribute:amr=mfa=x":        true, // the name runs to the first "="
		"":                           false,
		"attribute:":                 false,
		"attribute:amr":              false,
		"attribute:=mfa":             false,
		"attribute:amr=":             false,
	} {
		if got := upstreamAcrKeyProblem(key) == ""; got != ok {
			t.Errorf("%q: accepted = %v, want %v", key, got, ok)
		}
	}
}

// kit's trusted_email_domains entry form (kit pkg/lib/model/trusted_email_domains.go).
func TestTrustedEmailDomainPattern(t *testing.T) {
	for d, ok := range map[string]bool{
		"contoso.com":      true,
		"eu.contoso.co.uk": true,
		"a-b.example":      true,
		"contoso":          false,
		"Contoso.com":      false,
		"*.contoso.com":    false,
		"user@contoso.com": false,
		"contoso.com.":     false,
		"-contoso.com":     false,
		"contoso-.com":     false,
		"con_toso.com":     false,
	} {
		if got := trustedEmailDomainPattern.MatchString(d); got != ok {
			t.Errorf("%q: matched = %v, want %v", d, got, ok)
		}
	}
}
