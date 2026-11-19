package provider_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchema_OptionalAttributesClearOrAreExempt is audit T1's guard. An
// optional attribute that is also computed, with no default, keeps its last
// value when its line is removed from the configuration: deleting
// `link_by_verified_email = true` left accounts linking. Every such
// attribute, in every resource and nested block, must be on keepOnRemoval
// with its reason; anything else must be clearable (Optional alone, or a
// bool defaulting to false). A listed attribute that no longer has that
// shape fails too, so the list only ever names what is true.
//
// Scope identifiers (`tenant_id`, `realm_id` and the like) are left out:
// they replace the resource when configured and read back from the name.
func TestSchema_OptionalAttributesClearOrAreExempt(t *testing.T) {

	ctx := context.Background()
	p := provider.New("test")()

	kept := map[string]bool{}
	for _, f := range p.Resources(ctx) {
		r := f()
		meta := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "authwise"}, meta)
		resp := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, resp)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		walkKept(meta.TypeName, resp.Schema.Attributes, kept)
	}

	var missing, stale []string
	for name := range kept {
		if _, ok := keepOnRemoval[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name, reason := range keepOnRemoval {
		if !kept[name] {
			stale = append(stale, name)
		}
		assert.NotEmpty(t, reason, name)
	}
	sort.Strings(missing)
	sort.Strings(stale)

	assert.Empty(t, missing, "these optional attributes keep their value when removed from the configuration. "+
		"Make each clearable (gen/main.go withClearable, or Optional alone in a hand-written schema), "+
		"or list it in keepOnRemoval with the reason it may outlive its line")
	assert.Empty(t, stale, "these keepOnRemoval entries no longer keep their value (or are gone): remove them")
}

// walkKept records every attribute in the keep-on-removal shape, descending
// into the nested blocks a configuration can write.
func walkKept(prefix string, attrs map[string]schema.Attribute, kept map[string]bool) {

	for name, a := range attrs {
		path := prefix + "." + name
		if !a.IsOptional() {
			// Required, or computed only: nothing the configuration
			// removes, and its children are read, not written.
			continue
		}
		if a.IsComputed() && !hasDefault(a) && !isScopeID(name, a) {
			kept[path] = true
		}
		if n, ok := a.(schema.SingleNestedAttribute); ok {
			walkKept(path, n.Attributes, kept)
		}
		if n, ok := a.(schema.ListNestedAttribute); ok {
			walkKept(path, n.NestedObject.Attributes, kept)
		}
		if n, ok := a.(schema.SetNestedAttribute); ok {
			walkKept(path, n.NestedObject.Attributes, kept)
		}
	}
}

// hasDefault reports a Default, which a removed line plans instead of the
// state's value.
func hasDefault(a schema.Attribute) bool {
	d := reflect.ValueOf(a).FieldByName("Default")
	return d.IsValid() && !d.IsNil()
}

// isScopeID is a scope identifier: an `_id` attribute that replaces the
// resource when configured (tf.RequiresReplaceIfConfigured) and otherwise
// reads back from the name.
func isScopeID(name string, a schema.Attribute) bool {
	if !strings.HasSuffix(name, "_id") {
		return false
	}
	mods := reflect.ValueOf(a).FieldByName("PlanModifiers")
	if !mods.IsValid() {
		return false
	}
	for i := range mods.Len() {
		if strings.Contains(fmt.Sprintf("%T", mods.Index(i).Interface()), "requiresReplaceIfConfigured") {
			return true
		}
	}
	return false
}

// The six Guard config attributes guard-control never stores are deprecated
// as having no effect (audit T1); nothing else is.
func TestSchema_GuardConfigDeprecated(t *testing.T) {

	ctx := context.Background()
	p := provider.New("test")()

	var deprecated []string
	for _, f := range p.Resources(ctx) {
		r := f()
		meta := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "authwise"}, meta)
		resp := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, resp)
		for name, a := range resp.Schema.Attributes {
			if a.GetDeprecationMessage() != "" && name == "config" {
				deprecated = append(deprecated, meta.TypeName)
			}
		}
	}
	sort.Strings(deprecated)

	assert.Equal(t, []string{
		"authwise_guard_network", "authwise_guard_node", "authwise_guard_pool_relay",
		"authwise_guard_relay", "authwise_guard_relay_pool", "authwise_guard_resource",
	}, deprecated)
}
