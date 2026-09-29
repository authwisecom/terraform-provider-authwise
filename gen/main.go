// The generator entry point for the Authwise provider: the declarative spec
// table over the published apis pb types. Regenerate with
// `go generate ./...` or `go run .` from this directory.
//
// The behavioral layer here (Required / Immutable / Computed / Sensitive /
// JSON) is the provider's source of truth — kit protos carry no
// google.api.field_behavior annotations.
//
// Deferred entries (tracked in the GitLab plan):
//   - Event (read-only: tfinfra's data sources, singular and plural, still
//     require a Resource marker on the entry)
//   - ClientSecret (write-once hash/salt; needs write-only arguments)
//   - ProviderUsernamePassword config (repeated message field)
//
// Hand-written in internal/provider rather than generated, because their
// shapes are beyond tfinfra: authwise_secret (the material rides the create
// request and the :addVersion RPC, never the entity) and
// authwise_realm_authentication_policy (oneofs, repeated messages and
// durations nested several levels deep inside RealmConfig).
package main

//go:generate go run .

import (
	"reflect"

	accesspb "git.authwise.com/authwise/apis/authwise/access/v1alpha1"
	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
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

var (
	identityClient = reflect.TypeFor[identitypb.AuthwiseIdentityServiceClient]()
	accessClient   = reflect.TypeFor[accesspb.AuthwiseAccessServiceClient]()
)

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
		Type: reflect.TypeFor[E](),
		// Every resource gets its singular data source (Get by name) and its
		// plural one (every entity under a parent, through List).
		Implementations: append([]any{r, gentf.DataSource{}, gentf.DataSourceList{}}, impls...),
	}
}

// associate declares one authoritative association edge (the CLI's
// add-*/remove-*/list-* verb family becomes one set-valued resource).
func associate[T any]() gentf.Associate {
	return gentf.Associate{Target: reflect.TypeFor[T]()}
}

// access retargets an entry at the Access service client; the whole access
// surface is audience-scoped.
func access(r *gentf.Resource) {
	r.ClientType = accessClient
	r.Client = "access"
}

// callerNamed marks the entities kit keys by a name the caller supplies
// (the create request carries it in the entity's name field) rather than by
// a generated AWID: the resource gains a required "<type>_id" attribute.
func callerNamed(r *gentf.Resource) {
	r.CallerNamed = true
}

// withCollection overrides the derived AIP collection segment. kit's
// collections are kebab-case ("access-roles", "appearance-profiles"), while
// tfinfra derives the AIP-122 lower-camel default.
func withCollection(collection string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Collection = collection }
}

func withRequired(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Required = fields }
}

func withJSON(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.JSON = fields }
}

func withComputed(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Computed = fields }
}

func withImmutable(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Immutable = fields }
}

// withInputOnly marks the fields kit consumes but never echoes back.
// Without it the default Optional+Computed shape nulls them on every
// refresh, which for a create-only field means replacement on every plan.
func withInputOnly(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.InputOnly = fields }
}

// withDescription is what the resource is: its schema description, and so
// the first thing its documentation page says.
func withDescription(description string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Description = description }
}

// config declares a typed builder data source over a config message,
// masking the named fields in output.
func config[E any](typeName string, sensitive ...string) gentf.Entry {
	return gentf.Entry{
		Type: reflect.TypeFor[E](),
		Implementations: []any{
			gentf.ConfigDataSource{TypeName: typeName, Sensitive: sensitive},
		},
	}
}

func main() {

	gentf.NewRegistry().RunDirectoryPathHandler("../internal/generated", &gentf.Spec{
		Package: "generated",
		Entries: []gentf.Entry{
			// Tenant-scoped.
			crud[corepb.Domain](scopeTenant,
				withDescription("A domain the tenant serves logins on. The id is the domain name itself."), callerNamed, withJSON("config")),
			crud[corepb.Issuer](scopeTenant,
				withDescription("An OAuth 2.0 / OpenID Connect issuer: the login a set of clients shares, with its domain, token lifetimes and which realm or realms people sign in to (`config`)."), withJSON("config")),
			crud[corepb.Realm](scopeTenant,
				withDescription("A realm: a population of users with its own providers, factors and authentication policy."), withJSON("config")),
			crud[corepb.Theme](scopeTenant,
				withDescription("A tenant theme: the stylesheet and content the hosted login pages render with."), withJSON(
					"stylesheet_attributes", "content",
					"placeholder_stylesheet_attributes", "placeholder_content",
				)),
			crud[corepb.Asset](scopeTenant,
				withDescription("A static asset the hosted pages serve, such as a logo.")),
			// auth is a oneof of messages that each hold a SecretRef, which
			// is deeper than a typed nested attribute goes; it takes the JSON
			// lane, and a reference inside jsonencode still orders the
			// endpoint after the secret or issuer it names. tls is a plain
			// singular message and takes the typed nested lane; timeout is
			// the provider's first Duration, a string ("5s", "500ms") that
			// keeps the spelling written (kit#603, apis v0.9.0).
			crud[corepb.Endpoint](scopeTenant,
				withDescription("A service kit calls out to: its transport and address, how kit verifies it (`tls`), how kit authenticates to it (`auth`), and the per-call deadline (`timeout`)."), withJSON("auth")),
			// The SAML trust anchors (kit#487). certificate_pem is the
			// public certificate of any row, minted or imported, and is
			// read-only; importing a partner's PEM is a separate input-only
			// field on create, not an RPC of its own.
			crud[corepb.Certificate](scopeTenant,
				withDescription("A certificate and, when kit minted it, its private key. The trust anchor SAML connections verify signatures against, and the client certificate an endpoint presents for mTLS."),
				withRequired("display_name", "use"),
				withComputed("key_id", "origin", "subject", "not_before", "not_after",
					"fingerprint_sha256", "certificate_pem", "has_private_key"),
				// The mint parameters describe how to make the key pair, not
				// what was made, and kit only reads them on create.
				withImmutable("subject_common_name", "validity_days", "key_size", "import_certificate_pem"),
				withInputOnly("subject_common_name", "validity_days", "key_size", "import_certificate_pem")),

			// Realm-scoped.
			crud[corepb.User](scopeRealm,
				withDescription("A user in a realm."),
				withJSON("metadata", "extra_fields"),
				// origin is write-once and enrollment is derived and never
				// stored (kit#304, kit#324): kit refuses a differing value on
				// update rather than ignoring it, so neither may be written.
				// status stays writable — the default Optional+Computed shape
				// already covers its ""-means-unchanged semantics.
				withComputed("updated_at", "origin", "enrollment"),
			),
			crud[corepb.Provider](scopeRealm,
				withDescription("A way people sign in to a realm: username and password, a magic link, passkeys, a social or enterprise IdP, or SAML. `config` comes from the matching config data source."), withJSON("config")),
			// A second step the realm offers (kit#544). config is the
			// per-type Any; the factor_* config data sources below build it.
			crud[corepb.Factor](scopeRealm,
				withDescription("A second step a realm offers, such as TOTP, WebAuthn or Duo. `config` comes from the matching config data source; disabling keeps enrolled authenticators, destroying does not."),
				withRequired("display_name", "factor_type"),
				withImmutable("factor_type"),
				withJSON("config")),

			// Issuer-scoped.
			crud[corepb.Client](scopeIssuer,
				withDescription("An OAuth 2.0 client of an issuer: an application that signs people in or calls an API."), withJSON("config")),
			crud[corepb.Audience](scopeIssuer,
				withDescription("An API an issuer mints access tokens for, and the audience its Access catalog hangs off."), withJSON("config")),
			crud[corepb.AppearanceProfile](scopeIssuer,
				withDescription("An issuer's appearance profile: the stylesheet and content its login pages use."),
				withCollection("appearance-profiles"),
				withJSON("stylesheet_attributes", "content")),

			// Audience-scoped. The identity Role and Permission surface is
			// gone (apis v0.6.0): roles and permissions live in the Access
			// catalog, and a scope grants access permissions.
			crud[corepb.Scope](scopeAudience,
				withDescription("An OAuth scope on an audience, and the access permissions it grants. The id is the scope's own name."), callerNamed, associate[corepb.AccessPermission]()),

			// Access service (audience-scoped). AccessPermission and
			// AccessRole are name-keyed in kit — the catalog's vocabulary is
			// the deployer's to choose ("guardcontrol.tenants.get") — while
			// AccessCondition and AccessBinding are AWID-keyed.
			crud[corepb.AccessPermission](scopeAudience,
				withDescription("A permission in an audience's Access catalog. The id is the permission's own name, such as `guardcontrol.tenants.get`."), access, callerNamed,
				withCollection("access-permissions")),
			crud[corepb.AccessRole](scopeAudience,
				withDescription("A role in an audience's Access catalog. The id is the role's own name; its permissions are managed by `authwise_access_role_access_permissions`."), access, callerNamed,
				withCollection("access-roles"),
				associate[corepb.AccessPermission]()),
			crud[corepb.AccessCondition](scopeAudience,
				withDescription("A condition in an audience's Access catalog, which a binding can require."), access,
				withCollection("access-conditions")),
			crud[corepb.AccessBinding](scopeAudience,
				withDescription("A binding that grants a subject a role in an audience's Access catalog."), access,
				withCollection("access-bindings"),
				withRequired("subject_type", "subject_id", "role_name"),
				withComputed("created_by")),

			// Config builder data sources for Any-packed configs
			// (Provider.config, Client.config).
			{
				Type: reflect.TypeFor[corepb.ProviderAuthwise](),
				Implementations: []any{
					gentf.ConfigDataSource{},
				},
			},
			// The OAuth-shaped upstreams name their client secret by
			// reference (client_secret_ref, a Secret's name) since kit#370;
			// the material lives in an authwise_secret, not in the config.
			config[corepb.ProviderMicrosoft]("provider_microsoft"),
			config[corepb.ProviderGoogle]("provider_google"),
			config[corepb.ProviderGitHub]("provider_github"),
			config[corepb.ProviderLinkedIn]("provider_linkedin"),
			config[corepb.ProviderFacebook]("provider_facebook"),
			config[corepb.ProviderOkta]("provider_okta"),
			config[corepb.ProviderAuth0]("provider_auth0"),
			// apis v0.8.0's additions (kit#587). Apple's .p8 signing key is a
			// Secret named by private_key_ref; OIDC discovers its endpoints
			// unless they are pinned; OAuth has no id_token, so it reads a
			// userinfo document through its claim map.
			config[corepb.ProviderApple]("provider_apple"),
			config[corepb.ProviderOidc]("provider_oidc"),
			{
				Type: reflect.TypeFor[corepb.ProviderOAuth](),
				Implementations: []any{
					gentf.ConfigDataSource{
						TypeName:    "provider_oauth",
						Required:    []string{"identifier_source", "claim_map"},
						Description: "OAuth 2.0 with no id_token: explicit endpoints, a userinfo document, and a claim map over it (`userinfo.<path>` sources). X, Discord, Amazon, Twitch, Spotify and Bitbucket are recipes on this type.",
					},
				},
			},
			// SAML 2.0, both roles: ProviderSaml is kit as the SP (on a
			// Provider), SamlRelyingPartyConfig is kit as the IdP (on a
			// Client). Neither carries a client secret. claim_map is a
			// nested block rather than a JSON blob — tfinfra v0.0.10 renders
			// a singular message as a typed nested attribute.
			config[corepb.ProviderSaml]("provider_saml"),
			config[corepb.SamlRelyingPartyConfig]("saml_relying_party_config"),
			// The passwordless primaries (kit#194, kit#195). ttl is a
			// Duration, so a string ("10m"); ProviderPasskey has no fields
			// and its data source exists for the any envelope alone.
			{
				Type: reflect.TypeFor[corepb.ProviderMagicLink](),
				Implementations: []any{
					gentf.ConfigDataSource{
						TypeName:    "provider_magic_link",
						Description: "A one-time link and code sent by email. Unset fields take kit's defaults: a 6-digit code, a 10-minute ttl, `LINK_AND_CODE`.",
					},
				},
			},
			{
				Type: reflect.TypeFor[corepb.ProviderPasskey](),
				Implementations: []any{
					gentf.ConfigDataSource{
						TypeName:    "provider_passkey",
						Description: "The passkey provider signs nobody in without an enabled WebAuthn factor on the realm: kit refuses the first sign-in with \"realm offers no webauthn factor\". Declare an `authwise_factor` of type `webauthn` beside it.",
					},
				},
			},
			// Factor configs, one per type that has one (otp-email, otp-sms
			// and recovery-code take none).
			config[corepb.FactorTOTP]("factor_totp"),
			config[corepb.FactorWebAuthn]("factor_webauthn"),
			config[corepb.FactorDuo]("factor_duo"),
			{
				Type: reflect.TypeFor[corepb.FactorExternal](),
				Implementations: []any{
					gentf.ConfigDataSource{TypeName: "factor_external", JSON: []string{"config"}},
				},
			},
			{
				Type: reflect.TypeFor[corepb.InteractiveClientConfig](),
				Implementations: []any{
					gentf.ConfigDataSource{
						JSON: []string{"cors", "flow_integration_config"},
					},
				},
			},
		},
	})
}
