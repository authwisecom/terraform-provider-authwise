// The generator entry point for the Authwise provider: the declarative spec
// table over the published api-client-go pb types. Regenerate with
// `go generate ./...` or `go run .` from this directory.
//
// The behavioral layer here (Required / Immutable / Computed / Sensitive /
// JSON) is the provider's source of truth — kit protos carry no
// google.api.field_behavior annotations.
//
// Deferred entries (tracked in the GitLab plan):
//   - Event (read-only; needs standalone data sources / DataSourceList)
//   - ClientSecret (write-once hash/salt; needs write-only arguments)
//   - ProviderUsernamePassword config (repeated message field)
//   - access surface (AccessRole, AccessPermission, AccessCondition,
//     AccessBinding) — follows once the identity surface settles; its
//     AccessRole → access-permissions association edge rides with it
package main

//go:generate go run .

import (
	"reflect"

	identitypb "git.authwise.com/authwise/api-client-go/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/api-client-go/authwise/types/core/v1alpha1"
	gentf "github.com/activatedio/tfinfra/genlib/tf"
	tf "github.com/activatedio/tfinfra/pkg/tf"
)

// The Authwise scope table (mirrors kit's families.Scope).
var (
	scopeTenant   = tf.NewScope("tenants")
	scopeIssuer   = tf.NewScope("tenants", "issuers")
	scopeRealm    = tf.NewScope("tenants", "realms")
	scopeAudience = tf.NewScope("tenants", "issuers", "audiences")
)

var identityClient = reflect.TypeFor[identitypb.AuthwiseIdentityServiceClient]()

// resource builds the standard identity resource marker.
func resource(scope tf.Scope) gentf.Resource {
	return gentf.Resource{
		Scope:      scope,
		ClientType: identityClient,
		Client:     "identity",
	}
}

func crud[E any](scope tf.Scope, opts ...any) gentf.Entry {
	r := resource(scope)
	impls := []any{}
	for _, o := range opts {
		switch v := o.(type) {
		case func(r *gentf.Resource):
			v(&r)
		default:
			impls = append(impls, o)
		}
	}
	return gentf.Entry{
		Type:            reflect.TypeFor[E](),
		Implementations: append([]any{r, gentf.DataSource{}}, impls...),
	}
}

// associate declares one authoritative association edge (the CLI's
// add-*/remove-*/list-* verb family becomes one set-valued resource).
func associate[T any]() gentf.Associate {
	return gentf.Associate{Target: reflect.TypeFor[T]()}
}

func withJSON(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.JSON = fields }
}

func withComputed(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Computed = fields }
}

func withSensitive(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Sensitive = fields }
}

func providerConfig[E any](typeName string) gentf.Entry {
	return gentf.Entry{
		Type: reflect.TypeFor[E](),
		Implementations: []any{
			gentf.ConfigDataSource{TypeName: typeName, Sensitive: []string{"client_secret"}},
		},
	}
}

func main() {

	gentf.NewRegistry().RunDirectoryPathHandler("../internal/generated", &gentf.Spec{
		Package: "generated",
		Entries: []gentf.Entry{
			// Tenant-scoped.
			crud[corepb.Domain](scopeTenant, withJSON("config")),
			crud[corepb.Issuer](scopeTenant, withJSON("config")),
			crud[corepb.Realm](scopeTenant),
			crud[corepb.Theme](scopeTenant, withJSON(
				"stylesheet_attributes", "content",
				"placeholder_stylesheet_attributes", "placeholder_content",
			)),
			crud[corepb.AppearanceProfile](scopeTenant, withJSON("stylesheet_attributes", "content")),
			crud[corepb.Secret](scopeTenant, withSensitive("value")),
			crud[corepb.Asset](scopeTenant),
			crud[corepb.Endpoint](scopeTenant),

			// Realm-scoped.
			crud[corepb.User](scopeRealm,
				withJSON("metadata", "extra_fields"),
				withComputed("updated_at"),
				associate[corepb.Role](),
			),
			crud[corepb.Provider](scopeRealm, withJSON("config")),

			// Issuer-scoped.
			crud[corepb.Client](scopeIssuer, withJSON("config"), associate[corepb.Role]()),
			crud[corepb.Audience](scopeIssuer, withJSON("config")),

			// Audience-scoped.
			crud[corepb.Role](scopeAudience, associate[corepb.Permission]()),
			crud[corepb.Permission](scopeAudience),
			crud[corepb.Scope](scopeAudience, associate[corepb.Permission]()),

			// Config builder data sources for Any-packed configs
			// (Provider.config, Client.config).
			{
				Type: reflect.TypeFor[corepb.ProviderAuthwise](),
				Implementations: []any{
					gentf.ConfigDataSource{},
				},
			},
			providerConfig[corepb.ProviderMicrosoft]("provider_microsoft"),
			providerConfig[corepb.ProviderGoogle]("provider_google"),
			providerConfig[corepb.ProviderGitHub]("provider_github"),
			providerConfig[corepb.ProviderLinkedIn]("provider_linkedin"),
			providerConfig[corepb.ProviderFacebook]("provider_facebook"),
			providerConfig[corepb.ProviderDropbox]("provider_dropbox"),
			providerConfig[corepb.ProviderOkta]("provider_okta"),
			providerConfig[corepb.ProviderAuth0]("provider_auth0"),
			{
				Type: reflect.TypeFor[corepb.InteractiveClientConfig](),
				Implementations: []any{
					gentf.ConfigDataSource{
						JSON: []string{"cors", "logging", "flow_integration_config"},
					},
				},
			},
		},
	})
}
