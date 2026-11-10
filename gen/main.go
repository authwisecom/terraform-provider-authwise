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
//   - ProviderUsernamePassword config (repeated message field)
//
// Hand-written in internal/provider rather than generated, because their
// shapes are beyond tfinfra: authwise_secret (the material rides the create
// request and the :addVersion RPC, never the entity) and
// authwise_realm_authentication_policy (oneofs, repeated messages and
// durations nested several levels deep inside RealmConfig), and
// authwise_service_account with authwise_service_account_secret (the
// account's secrets are a repeated message, and a secret has no read or
// delete of its own).
package main

//go:generate go run .

import (
	"reflect"

	accesspb "git.authwise.com/authwise/apis/authwise/access/v1alpha1"
	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
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
	scopeClient   = tf.NewScope("tenants", "issuers", "clients")

	// Guard's (guard-control): a network in a kit tenant (tf#65).
	scopeGuardNetwork = tf.NewScope("tenants", "networks")
)

// References: the bare-AWID fields and what they hold, from kit's
// RESOURCE_IDENTITY §4 (the rows kit#620 admits by scope) and the AWID
// prefixes in kit's pkg/lib/model/prefix.go. Each is validated in plan, so
// a full name pasted into one fails there with the attribute to use rather
// than at apply. By hand until kit#619 puts google.api.resource_reference
// on the fields.
//
// The SAML configs' certificate fields are left out. kit takes a
// Certificate's full name there as well as its id, since its own metadata
// import writes the name (§4.5), and configurations written against it pass
// `.name`; validating the id alone would break them. They join when kit
// settles that exception, at its next breaking tag (D8).
var (
	refAudience          = gentf.Reference{Target: "audience", Prefix: "a"}
	refAppearanceProfile = gentf.Reference{Target: "appearance_profile", Prefix: "ap"}
	refTheme             = gentf.Reference{Target: "theme", Prefix: "th"}
	refAccessCondition   = gentf.Reference{Target: "access_condition", Prefix: "axc"}
	refDomain            = gentf.Reference{Target: "domain", Prefix: "d"}

	// The parent attributes: every resource's and every plural data
	// source's.
	scopeReferences = map[string]gentf.Reference{
		"tenant_id":   {Target: "tenant", Prefix: "t"},
		"issuer_id":   {Target: "issuer", Prefix: "i"},
		"realm_id":    {Target: "realm", Prefix: "r"},
		"audience_id": refAudience,
		"client_id":   {Target: "client", Prefix: "c"},
		"network_id":  {Target: "guard_network", Prefix: "n"},
	}
)

var (
	identityClient = reflect.TypeFor[identitypb.AuthwiseIdentityServiceClient]()
	accessClient   = reflect.TypeFor[accesspb.AuthwiseAccessServiceClient]()
	guardClient    = reflect.TypeFor[guardpb.GuardControlServiceClient]()
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
	list := gentf.DataSourceList{}
	impls := []any{}
	for _, o := range opts {
		switch v := o.(type) {
		case func(r *gentf.Resource):
			v(&r)
		case gentf.DataSourceList:
			list = v
		default:
			impls = append(impls, o)
		}
	}
	return gentf.Entry{
		Type: reflect.TypeFor[E](),
		// Every resource gets its singular data source (Get by name) and its
		// plural one (every entity under a parent, through List).
		Implementations: append([]any{r, gentf.DataSource{}, list}, impls...),
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

// guard retargets an entry at guard-control and names it authwise_guard_<typeName>:
// Guard's entity names (Tenant, Resource, User) would otherwise collide with
// kit's, and its pages group under Guard by the prefix.
func guard(typeName string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) {
		r.ClientType = guardClient
		r.Client = "guard"
		r.TypeName = "guard_" + typeName
	}
}

// guardList is the plural data source of a Guard entry, prefixed like its
// resource.
func guardList(typeName string) gentf.DataSourceList {
	return gentf.DataSourceList{TypeName: "guard_" + typeName}
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

// withOps narrows the operations an entry's API exposes; the zero value is
// all of them.
func withOps(ops gentf.Ops) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Ops = ops }
}

func withRequired(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Required = fields }
}

func withJSON(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.JSON = fields }
}

func withSensitive(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Sensitive = fields }
}

func withComputed(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Computed = fields }
}

func withImmutable(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Immutable = fields }
}

// withVolatile marks computed fields something other than the resource's own
// write may change: an update plans them unknown rather than promising the
// prior value.
func withVolatile(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Volatile = fields }
}

// withInputOnly marks the fields kit consumes but never echoes back.
// Without it the default Optional+Computed shape nulls them on every
// refresh, which for a create-only field means replacement on every plan.
func withInputOnly(fields ...string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.InputOnly = fields }
}

// withReferences declares the entity's fields that hold another row's id.
func withReferences(refs map[string]gentf.Reference) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.References = refs }
}

// withDescription is what the resource is: its schema description, and so
// the first thing its documentation page says.
func withDescription(description string) func(r *gentf.Resource) {
	return func(r *gentf.Resource) { r.Description = description }
}

// clientSecret is minted rather than created (kit#617): MintClientSecret
// returns the secret exactly once, beside the row, and no read returns it
// again, so it lives in state as a sensitive attribute (the ruling on #18).
// It is built by hand rather than through crud because a minted entry takes
// no singular data source: that would share the model, secret and all.
//
// hash_enabled is kit's to set; the mint takes only expires_at, which
// patches in place. keepers is the rotation trigger, since kit offers no
// rotate verb.
func clientSecret() gentf.Entry {
	r := resource(scopeClient)
	r.Collection = "client-secrets"
	r.Computed = []string{"hash_enabled"}
	r.Description = "A client's secret, for authenticating it to the token endpoint. " +
		"kit creates it and returns it once, so the secret is kept in state: treat the state as sensitive."
	r.Mint = &gentf.Mint{Method: "MintClientSecret", Once: []string{"secret"}}
	return gentf.Entry{
		Type:            reflect.TypeFor[corepb.ClientSecret](),
		Implementations: []any{r, gentf.DataSourceList{}},
	}
}

// samlBindings are the SAML 2.0 bindings kit sends and receives on, as the
// URNs it takes, with the short names its documentation uses standing for
// them.
var samlBindings = gentf.Values{
	Allowed: []string{
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect",
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
	},
	Aliases: map[string]string{
		"HTTP-Redirect": "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect",
		"HTTP-POST":     "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
	},
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
		Package:          "generated",
		ProviderTypeName: "authwise",
		ScopeReferences:  scopeReferences,
		Entries: []gentf.Entry{
			// Tenant-scoped.
			// A hostname the tenant serves, native or custom, keyed by an
			// AWID since kit#808; purpose (issuer, guard, console) since
			// kit#942, where product (guard, kit#903) was. The resource is
			// hand-written in internal/provider (the custom host's waits);
			// the data sources are these.
			crud[corepb.Domain](scopeTenant,
				withDescription("A hostname of the tenant: native, an Authwise host; custom (`custom`), a hostname of yours; or custom with `purpose = \"guard\"` or `\"console\"`, a hostname of yours serving the tenant's Guard address or its console. `product` is its predecessor, read out for one release (kit#942). Issuers answer at a domain through `authwise_issuer_domain_binding` (kit#911); `bindings` lists them."),
				withRequired("hostname"), withImmutable("hostname", "issuer_id", "product", "purpose", "custom"),
				withComputed("state", "dns_records", "failure", "certificate_expires_at", "activated_at", "claim_expires_at", "created_at", "traffic_target", "bindings"),
				withJSON("config"),
				withReferences(map[string]gentf.Reference{"issuer_id": scopeReferences["issuer_id"]})),
			// domain_id names the issuer's native host (kit#808, D12);
			// domain_name is its hostname, read back. kit still takes
			// domain_name on create until kit#810, but the provider sends
			// domain_id only.
			// account_client_id is the account page's client, which kit
			// creates at the issuer's first audience (kit#827).
			// preferred_domain_id is the host kit builds mailed links on
			// (kit#857), read back here and written by
			// authwise_issuer_preferred_domain: a custom domain names its
			// issuer, so the issuer naming it back would be a cycle (#52).
			// That resource's destroy runs before an update of the issuer
			// in the same apply, so the issuer plans it unknown: volatile.
			// kit#911: an issuer reaches hosts through
			// authwise_issuer_domain_binding. domain_id and path are a
			// deprecated create-time convenience (the first binding),
			// output only afterwards; preferred_binding_id replaces
			// preferred_domain_id, written by authwise_issuer_preferred_binding
			// for the same cycle reason. bindings and domain_name (the first
			// binding's host) change with binding resources in the same
			// apply: volatile too.
			crud[corepb.Issuer](scopeTenant,
				withDescription("An OAuth 2.0 / OpenID Connect issuer: the login a set of clients shares, with its token lifetimes and which realm or realms people sign in to (`config`). It answers at the hosts its `authwise_issuer_domain_binding`s name (kit#911). Its appearance is whichever of its profiles is the default (`authwise_appearance_profile.is_default`)."), withJSON("config"),
				withImmutable("domain_id", "path"), withComputed("account_client_id"), withVolatile("preferred_domain_id", "preferred_binding_id", "bindings", "domain_name"),
				withReferences(map[string]gentf.Reference{"domain_id": refDomain})),
			crud[corepb.Realm](scopeTenant,
				withDescription("A realm: a population of users with its own providers, factors and authentication policy."), withJSON("config")),
			crud[corepb.Theme](scopeTenant,
				withDescription("A tenant theme: the stylesheet and content the hosted login pages render with. Start from a layout's starter (`GET {issuer}/api/v1/ui/layouts`): its stylesheet template, `stylesheet_attributes` and `content`, and its two schemas. `layout` is fixed at creation; kit refuses a change, so another layout is another theme. Colours in `stylesheet_attributes` are six hex digits without `#`. An asset is named in `content` by its served URL, `/assets` plus the asset's `path`."), withJSON(
					"stylesheet_attributes", "content",
					"placeholder_stylesheet_attributes", "placeholder_content",
				)),
			crud[corepb.Asset](scopeTenant,
				withDescription("A static asset the hosted pages serve, such as a logo. This is the asset's metadata; upload the file itself with `authwise_asset_content`.")),
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
				withDescription("A certificate and, when kit created it, its private key. The trust anchor SAML connections verify signatures against, and the client certificate an endpoint presents for mTLS."),
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
				// The address and phone fields are output only (kit#662,
				// kit#666): kit refuses them on create, update and patch
				// whatever the value. An address now comes from an
				// invitation (InviteUser). credentials_changed_at is kit's.
				withComputed("updated_at", "origin", "enrollment",
					"email", "email_verified", "phone_number", "phone_number_verified",
					"credentials_changed_at"),
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
			// An issuer's host and path (kit#911, TENANT_DOMAINS.md): one
			// (domain, path) per binding, unique in the install, each its own
			// iss. There is no update: a binding is replaced. issuer_id is the
			// parent, read back from the name like every scope parent.
			crud[corepb.IssuerDomainBinding](scopeIssuer,
				withDescription("Where an issuer answers: one of the tenant's domains at a path, so the issuer's URL (`iss`) is `https://<hostname><path>`. An issuer may have no binding, one, or several, each its own `iss`; one domain serves several issuers at different paths. There is no update: changing `domain_id` or `path` replaces the binding."),
				withCollection("issuer-domain-bindings"),
				withOps(gentf.OpGet|gentf.OpList|gentf.OpCreate|gentf.OpDelete),
				withRequired("domain_id"), withImmutable("domain_id", "path"),
				withComputed("hostname", "url", "routable", "created_at"),
				withReferences(map[string]gentf.Reference{"domain_id": refDomain})),
			crud[corepb.Client](scopeIssuer,
				withDescription("An OAuth 2.0 client of an issuer: an application that signs people in or calls an API."), withJSON("config"),
				// owning_tenant_id marks a service account (kit#756): kit
				// sets it on CreateServiceAccount and refuses it here.
				withComputed("owning_tenant_id"),
				withReferences(map[string]gentf.Reference{"audience_id": refAudience, "appearance_profile_id": refAppearanceProfile})),
			clientSecret(),
			crud[corepb.Audience](scopeIssuer,
				withDescription("An API an issuer issues access tokens for, and the audience its Access catalog hangs off."), withJSON("config"),
				withReferences(map[string]gentf.Reference{"appearance_profile_id": refAppearanceProfile})),
			crud[corepb.AppearanceProfile](scopeIssuer,
				// is_default is the issuer's appearance (kit#680, apis v0.18.0).
				// kit holds exactly one default per issuer and moves it only
				// through MakeDefaultAppearanceProfile; the wrapper in
				// internal/provider routes the flag there.
				withDescription("An issuer's appearance profile: a theme plus overrides of its `stylesheet_attributes` and `content`, key by key. The issuer's default profile (`is_default`) is the issuer's appearance; a client's or audience's `appearance_profile_id` overrides it."),
				withCollection("appearance-profiles"),
				withJSON("stylesheet_attributes", "content"),
				withReferences(map[string]gentf.Reference{"theme_id": refTheme})),

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
			// A declared resource type (kit#25): a type of the deployer's
			// own, and where its parent edge comes from. Name-keyed, like a
			// permission. Every field may change in place.
			crud[corepb.AccessResourceType](scopeAudience,
				withDescription("A resource type declared in an audience's Access catalog, such as `guardcontrol.tenant`: its `parent_type` and where the parent edge comes from (`expansion_mode`: `NONE`, `PUSH`, `PULL` or `PATH`). A binding anchored at a parent reaches the resources linked under it. The id is the type's own name."), access, callerNamed,
				withCollection("access-resource-types")),
			crud[corepb.AccessCondition](scopeAudience,
				withDescription("A condition in an audience's Access catalog, which a binding can require."), access,
				withCollection("access-conditions")),
			crud[corepb.AccessBinding](scopeAudience,
				withDescription("A binding that grants a subject a role in an audience's Access catalog.\n\n"+
					"**Guard's administrative roles** (`guardcontrol.tenantAdmin`, `guardcontrol.networkAdmin`, `guardcontrol.viewer`) are bound in the tenant's administration audience, which needs home administration on the tenant (`authwise_tenant_home_administration`). "+
					"One of them in the tenant's Guard audience is refused at plan with the reason, and by kit; the Guard audience keeps `guardcontrol.user` and devices only. "+
					"The tenant's central administrators (its owner and `tenant.admin` seats) get Guard's tenant administrator role from kit, from their seat, not from a binding (kit#940, tf#72)."), access,
				withCollection("access-bindings"),
				withRequired("subject_type", "subject_id", "role_name"),
				withComputed("created_by"),
				withReferences(map[string]gentf.Reference{"condition_id": refAccessCondition})),

			// Guard (guard-control, #32). Its rows are keyed by kit's tenant
			// (`t-…`) since the tenant collapse (apis v0.57.0, tf#65): there is
			// no Guard tenant. Guard's settings for a kit tenant are the
			// hand-written authwise_guard_tenant_settings.
			// cidr cannot change once the network has a node; replacing the
			// network is the only way to move it. mesh_domain (apis v0.48.0)
			// is derived from the network's and its tenant's slugs, so a
			// slug change moves it.
			crud[guardpb.Network](scopeTenant,
				withDescription("A Guard network: a WireGuard mesh in a tenant. `cidr` is its address space inside 100.64.0.0/10, the whole of it when unset; changing it replaces the network."),
				guard("network"), guardList("networks"),
				withImmutable("cidr"),
				// pool_capped (guard-control#88): the tenant's plan cap on
				// relay bytes reached, read only like pool_killed.
				withComputed("mesh_domain", "pool_killed", "pool_killed_at", "pool_capped", "pool_capped_at"),
				withVolatile("mesh_domain", "pool_killed", "pool_killed_at", "pool_capped", "pool_capped_at"),
				withJSON("config")),
			// A relay's credential (:issueCredential, apis v0.59.0) is the
			// hand-written wrapper's in internal/provider: issued once and
			// kept, since a second issue rotates. has_credential and the
			// report fields move with the relay, not with a write.
			crud[guardpb.Relay](scopeGuardNetwork,
				withDescription("A relay a Guard network's nodes connect through when no direct path exists. guard-control serves a network's relays to its nodes, lowest `priority` first."),
				guard("relay"), guardList("relays"),
				withRequired("url"),
				withComputed("last_seen_at", "has_credential", "last_report_at", "reported_version"),
				withVolatile("has_credential", "last_report_at", "reported_version"),
				withJSON("config")),
			// Estate relay pools (guard-control#81, tf#66): top-level, kebab
			// collections per kit's naming rule (relay-pools/rp-…,
			// pool-relays/rl-…), and
			// operator-only, so a tenant credential is refused at apply. A
			// pool relay is a flat collection naming its pool; moving it is
			// an update of pool.
			crud[guardpb.RelayPool](tf.ScopeNone,
				withDescription("An estate relay pool: a named set of relays shared across networks and tenants. Operators only. `select` orders the pool's relays for a node, `served-order` (priority, then display name; the default) or `nearest` (by the node's `region`). `default` makes it the estate default pool: every tenant may use it, and it is the default of a tenant without its own (`authwise_guard_relay_pool_access.default_pool`). Setting it on one pool clears it on the previous default. A pool in use is not deleted (`RELAY_POOL_IN_USE`): remove it from networks and from tenants' access first."),
				guard("relay_pool"), guardList("relay_pools"), withCollection("relay-pools"),
				withComputed("relay_count"), withVolatile("relay_count"), withJSON("config")),
			crud[guardpb.PoolRelay](tf.ScopeNone,
				withDescription("A relay of an estate relay pool. Operators only. Changing `pool` moves the relay; it learns the move on its next report."),
				guard("pool_relay"), guardList("pool_relays"), withCollection("pool-relays"),
				withRequired("pool", "url"),
				withComputed("has_credential", "last_report_at", "reported_version"),
				withVolatile("has_credential", "last_report_at", "reported_version"),
				withJSON("config")),
			// serving_node_ids is applied on create and otherwise only
			// echoed; authwise_guard_resource_nodes owns the set, so here it
			// is read-only. addresses (apis v0.48.0) is what the serving
			// nodes last resolved an application to: output-only, and moved
			// by the nodes rather than by a write.
			crud[guardpb.Resource](scopeGuardNetwork,
				withDescription("A routed target in a Guard network that is not a node: a subnet, a host or an application. The nodes that serve it are managed by `authwise_guard_resource_nodes`."),
				guard("resource"), guardList("resources"),
				withRequired("kind", "address"), withImmutable("kind"),
				withComputed("serving_node_ids", "addresses"), withVolatile("addresses"), withJSON("config"),
				gentf.Associate{Target: reflect.TypeFor[guardpb.Node](), Attribute: "nodes", TypeName: "guard_resource_nodes"}),
			// A node terraform declares is a pre-registered server (#32 stage
			// 2). Created without a public_key it is PENDING, and the create
			// response alone carries its auth code, which the wrapper in
			// internal/provider keeps. Enrolment then sets public_key and
			// endpoint, so both are optional and computed: what the device
			// wrote reads back without a diff. The address is allocated
			// when left out and never changes. :issueAuthCode and :revoke
			// are not modelled, and a person's node (owner_id set) is never
			// terraform's.
			crud[guardpb.Node](scopeGuardNetwork,
				withDescription("A server's node in a Guard network, registered ahead of time. Created without a `public_key`, it is `PENDING` and its one-time `auth_code` enrolls the server; created with one, it is `ACTIVE` at once. The auth code is shown once, so it is kept in state: treat the state as sensitive. People's nodes join when they sign in and are not managed here."),
				guard("node"), guardList("nodes"),
				withImmutable("address"),
				withComputed("allowed_ips", "owner_id", "state", "roles", "auth_code", "auth_code_expires_at", "last_seen_at", "platform"),
				withSensitive("auth_code"),
				withJSON("config")),
			// No Invite: Guard's Invite entity is retired (kit#902 D5, tf#67).
			// A person is invited by kit's InviteUser and granted with
			// GrantEnrolment.

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
						TypeName: "provider_oauth",
						Required: []string{"identifier_source", "claim_map"},
						Descriptions: map[string]string{
							"claim_map": "Required, with no default: sources are `userinfo.<path>`.",
						},
						Description: "OAuth 2.0 with no id_token: explicit endpoints, a userinfo document, and a claim map over it (`userinfo.<path>` sources). X, Discord, Amazon, Twitch, Spotify and Bitbucket are recipes on this type.",
					},
				},
			},
			// SAML 2.0, both roles: ProviderSaml is kit as the SP (on a
			// Provider), SamlRelyingPartyConfig is kit as the IdP (on a
			// Client). Neither carries a client secret. claim_map is a
			// nested block rather than a JSON blob — tfinfra v0.0.10 renders
			// a singular message as a typed nested attribute.
			//
			// The bindings are plain strings in the proto holding a SAML
			// binding URN. A short name ("HTTP-Redirect", as kit's own
			// comments spell it) applied cleanly and broke every sign-in
			// at /authorize (#53), so both take the URNs at plan, and the
			// short names stand for them.
			{
				Type: reflect.TypeFor[corepb.ProviderSaml](),
				Implementations: []any{
					gentf.ConfigDataSource{
						TypeName: "provider_saml",
						Values:   map[string]gentf.Values{"idp_sso_binding": samlBindings},
						Descriptions: map[string]string{
							"idp_sso_binding":                    "The binding on the IdP's SingleSignOnService, which is how kit sends it an AuthnRequest, as the SAML binding URN. Unset is HTTP-Redirect. Read it off the IdP's metadata: it describes their endpoint, not a choice made here.",
							"idp_metadata_imported_at":           "When kit last imported the IdP's metadata. kit sets it on import; leave it unset.",
							"signing_certificate_id":             "Certificate resource name kit signs AuthnRequests with, and decrypts encrypted assertions with.",
							"sign_authn_requests":                "Sign every AuthnRequest with `signing_certificate_id`.",
							"allow_create":                       "Sets AllowCreate on the requested NameIDPolicy: the IdP may create an identifier for a person it has none for.",
							"want_assertions_signed":             "Require every assertion to be signed. kit requires it anyway unless `want_response_signed` is set: at least one of the two always holds, since an SP that verifies nothing accepts assertions from anyone.",
							"want_response_signed":               "Require the Response itself to be signed. See `want_assertions_signed`.",
							"identifier_source":                  "Which part of the assertion becomes the user's identifier, as one of two paths: `assertion.name_id`, the Subject's NameID (the default), or `assertion.attributes.<name>`, the first value of the attribute `<name>`, e.g. `assertion.attributes.email`. A partner whose NameID is transient or opaque needs the second form: the identifier links a returning person to their user, so it must be stable across sign-ins.",
							"identifier_hint":                    "How the identifier that routed the person to this partner (the address typed at an identifier-first login, or `login_hint` on /authorize) reaches it, so the partner need not ask for it again. Nothing is sent when no identifier was routed. One of `LOGIN_HINT_QUERY` (the default): a `login_hint` query parameter on the IdP's SSO URL, which is what Entra ID, Google Workspace and Okta document; `SUBJECT`: a `<saml:Subject>` in the AuthnRequest, for an IdP that reads one (Entra ID refuses the whole request with AADSTS900236); or `NONE`: send nothing, and the partner asks the person.",
							"requested_authn_context":            "The RequestedAuthnContext kit sends in the AuthnRequest. Unset requests nothing and leaves the partner to authenticate the person however its own policy says. That is the default because an exact `PasswordProtectedTransport` fails anyone who signs in to the partner without a password, such as an Entra ID user on passwordless or MFA (AADSTS75011). When set, `class_refs` must hold at least one non-blank URI.",
							"requested_authn_context.comparison": "One of `exact`, `minimum`, `better` or `maximum`. Unset means `exact`, as SAML defines. kit refuses any other value.",
							"requested_authn_context.class_refs": "AuthnContextClassRef URIs, in order of preference, e.g. `urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport`. At least one is required, and none may be blank.",
						},
					},
				},
			},
			{
				Type: reflect.TypeFor[corepb.SamlRelyingPartyConfig](),
				Implementations: []any{
					gentf.ConfigDataSource{
						TypeName: "saml_relying_party_config",
						Values:   map[string]gentf.Values{"acs_binding": samlBindings},
						Descriptions: map[string]string{
							"acs_binding":             "How kit delivers the Response to the partner's AssertionConsumerService, as the SAML binding URN. Unset is HTTP-POST.",
							"sp_metadata_imported_at": "When kit last imported the partner's metadata. kit sets it on import; leave it unset.",
						},
					},
				},
			},
			// The passwordless primaries (kit#194, kit#195). ttl is a
			// Duration, so a string ("10m"); ProviderPasskey has no fields
			// and its data source exists for the any envelope alone.
			// link_scope and unknown_address (kit#817, kit#825; apis
			// v0.41.0) each trade something away, and tfinfra has no
			// per-attribute description for a config field, so the trade
			// is said here, one sentence each. request_limits (kit#831,
			// apis v0.43.0) has no plan-time range check: tfinfra emits
			// validators only for enums, so kit refuses out of bounds at
			// apply, as it does code_length.
			{
				Type: reflect.TypeFor[corepb.ProviderMagicLink](),
				Implementations: []any{
					gentf.ConfigDataSource{
						TypeName: "provider_magic_link",
						Description: "A one-time link and code sent by email. Unset fields take kit's defaults: a 6-digit code, a 10-minute ttl, `LINK_AND_CODE`, `SAME_BROWSER`, `SILENT`. " +
							"`link_scope = \"ANY_BROWSER\"` lets the link sign in whichever browser opens it, after the person confirms, at the price that whoever reads the mailbox can sign in anywhere, so pair it with a step-up rule (it needs `LINK_AND_CODE`). " +
							"`unknown_address = \"TELL\"` tells a person whose address has no account so, where the realm would not create one, at the price of revealing which addresses have accounts at the request throttle's rate. " +
							"`request_limits` caps sign-in requests per hour per address (1 to 20, default 3), per IP (1 to 100, default 10) and per realm (1 to 10000, default 500); 0 or unset takes the default, there is no unlimited, and a higher cap lets more mail through to a person's inbox or to an attacker probing addresses.",
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
						// kit writes metadata_document on the clients it
						// derives from a Client ID Metadata Document (kit#413),
						// and a value set on any other client changes nothing
						// (tf#63).
						Omit: []string{"metadata_document"},
						// The proto documents the two refresh lifetimes in one
						// comment, on the first, in kit's internal terms.
						Descriptions: map[string]string{
							"allowed_redirect_uris":                 "The redirect URIs /authorize accepts for this client.",
							"access_token_expire_seconds":           "Access token lifetime in seconds. Unset takes the value from the layer above (tenant, issuer, audience).",
							"refresh_token_expire_seconds":          "Refresh token idle lifetime in seconds: it slides forward on every rotation. Unset takes the value from the layer above.",
							"refresh_token_absolute_expire_seconds": "Hard cap on a refresh token family in seconds, set once when the family is created and never extended. Unset takes the value from the layer above.",
							"cors":                                  "The browser origins allowed to call kit for this client.",
							"flow_integration_config":               "The endpoint kit calls during this client's login flow.",
							"require_consent":                       "Make /authorize ask the person to approve this client for the scopes it requests. Default false. `prompt=consent` asks regardless, so this is the floor, not the switch.",
						},
					},
				},
			},
		},
	})
}
