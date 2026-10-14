package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// jsonAttribute is a config data source's output: the message's protojson.
const jsonAttribute = "json"

var _ datasource.DataSource = &configJSONDataSource{}

// configJSONDataSource builds a config message from typed attributes and
// exposes its protojson as `json`, for the resource attributes that hold a
// plain message as JSON (#38): authwise_issuer.config and the like. It is
// the counterpart of the generated config data sources, whose `any` is the
// Any envelope; those are tfinfra's, which refuses oneofs and packs into an
// Any, so these are built here on protoattr. It makes no API call.
//
// Writing the JSON by hand still works. What this adds is a schema: keys in
// snake_case and checked at plan, enum values validated, durations as Go
// strings, and zero values left out of the encoding, as kit leaves them out
// of what it returns.
type configJSONDataSource struct {
	spec configJSONSpec
}

// configJSONSpec is one config data source.
type configJSONSpec struct {
	// typeName is the Terraform type without the provider prefix.
	typeName string
	// message is the config message.
	message proto.Message
	// attribute is the resource attribute the output is written to, for
	// the description ("authwise_issuer.config").
	attribute string
	// description leads the schema description.
	description string
	// descriptions documents fields by dotted path, as protoAttributes.
	descriptions map[string]string
	// omit names top-level fields left out of the schema, because another
	// resource owns them.
	omit map[string]bool
}

func newConfigJSONDataSource(spec configJSONSpec) func() datasource.DataSource {
	return func() datasource.DataSource { return &configJSONDataSource{spec: spec} }
}

func (d *configJSONDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.spec.typeName
}

func (d *configJSONDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {

	md := d.spec.message.ProtoReflect().Descriptor()
	attrs := protoDataSourceAttributes(md, d.spec.descriptions, d.spec.omit)

	if _, ok := attrs[jsonAttribute]; ok {
		panic(fmt.Sprintf("%s: a field named %q collides with the output", md.FullName(), jsonAttribute))
	}
	attrs[jsonAttribute] = schema.StringAttribute{
		Computed:            true,
		CustomType:          jsontypes.NormalizedType{},
		MarkdownDescription: fmt.Sprintf("The protojson encoding of %s, for `%s`.", md.Name(), d.spec.attribute),
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: withSuffix(d.spec.description, fmt.Sprintf(
			"Builds the %s message and exposes its protojson encoding as `json`, the value `%s` takes. Makes no API calls.",
			md.Name(), d.spec.attribute)),
		Attributes: attrs,
	}
}

func (d *configJSONDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	m := d.spec.message.ProtoReflect().New()
	if err := valueToProto(req.Config.Raw, m); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("invalid %s", m.Descriptor().Name()), err.Error())
		return
	}

	doc, err := configJSON(m.Interface())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("cannot encode %s", m.Descriptor().Name()), err.Error())
		return
	}

	resp.State.Raw = req.Config.Raw
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(jsonAttribute), jsontypes.NewNormalizedValue(doc))...)
}

// configJSON is m's protojson, re-encoded so the same message always reads
// as the same string: protojson varies its whitespace from build to build
// on purpose, and encoding/json sorts the keys.
func configJSON(m proto.Message) (string, error) {

	b, err := protojson.Marshal(m)
	if err != nil {
		return "", err
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return "", err
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// The token lifetimes kit resolves tenant, issuer, audience, client: zero
// leaves the field to the next layer out.
var tokenLifetimeDescriptions = map[string]string{
	"access_token_expire_seconds":           "Access token lifetime in seconds. Unset takes the value from the layer above (tenant, issuer, audience, client).",
	"refresh_token_expire_seconds":          "Refresh token idle lifetime in seconds: it slides forward on every rotation. Unset takes the value from the layer above.",
	"refresh_token_absolute_expire_seconds": "Hard cap on a refresh token family in seconds, set once when the family is created. Unset takes the value from the layer above.",
	"cors":                                  "The browser origins allowed to call kit.",
	"cors.allowed_origins":                  "Origins allowed to call kit from a browser.",
	"flow_integration_config":               "The endpoint kit calls during the login flow.",
	"cors.allowed_origins_regexp":           "Origin patterns allowed to call kit from a browser.",
	"flow_integration_config.endpoint_name": "The endpoint's full resource name (`tenants/{t}/endpoints/{e}`).",
}

func withTokenLifetimes(d map[string]string) map[string]string {
	for k, v := range tokenLifetimeDescriptions {
		if _, ok := d[k]; !ok {
			d[k] = v
		}
	}
	return d
}

// configJSONSpecs are the config data sources, one per resource attribute
// that holds a typed message as JSON. DomainConfig has no fields yet, and
// the Guard entities' configs are google.protobuf.Struct, with no schema to
// derive.
var configJSONSpecs = []configJSONSpec{
	{
		typeName:    "issuer_config",
		message:     &corepb.IssuerConfig{},
		attribute:   "authwise_issuer.config",
		description: "An issuer's config: its token lifetimes, CORS, and which realm or realms people sign in to (one of the three provider selectors).",
		descriptions: withTokenLifetimes(map[string]string{
			"account_client_id":                                          "The issuer's account-page client: the public PKCE client the account page signs in with.",
			"single_realm_provider_selector":                             "Every login goes to one realm.",
			"single_realm_provider_selector.realm_name":                  "The realm's full resource name, e.g. `authwise_realm.x.name`.",
			"multi_realm_provider_selector":                              "Identifier-first: the login page asks for an identifier, and ordered rules route it to a realm. kit refuses a selector it could not honour, naming the field.",
			"multi_realm_provider_selector.realm_names":                  "The realms this issuer serves, as full resource names. Every target must name one of them.",
			"multi_realm_provider_selector.identifier":                   "The identifier screen.",
			"multi_realm_provider_selector.identifier.label":             "The field's label. Unset follows `kind`.",
			"multi_realm_provider_selector.identifier.kind":              "`EMAIL` (the default when unset), `USERNAME` or `ANY`.",
			"multi_realm_provider_selector.rules":                        "Ordered; the first match wins.",
			"multi_realm_provider_selector.rules.name":                   "Unique within the selector, and recorded as `routing_rule` on each login's audit row.",
			"multi_realm_provider_selector.rules.domains":                "Exact (`partner.example`) or subdomain (`*.partner.example`) domains, lower-case A-labels, matched against the part of the identifier after its last `@`.",
			"multi_realm_provider_selector.rules.condition":              "CEL over `identifier`, `local_part` and `domain` returning a bool. With `domains`, both must hold.",
			"multi_realm_provider_selector.rules.target":                 "Where a matching identifier goes.",
			"multi_realm_provider_selector.rules.target.realm_name":      "The realm, one of `realm_names`.",
			"multi_realm_provider_selector.rules.target.provider_name":   "A provider in that realm. Unset, the realm's own selection applies.",
			"multi_realm_provider_selector.default_target":               "Where an identifier that matches no rule goes. Required by kit.",
			"multi_realm_provider_selector.default_target.realm_name":    "The realm, one of `realm_names`.",
			"multi_realm_provider_selector.default_target.provider_name": "A provider in that realm. Unset, the realm's own selection applies.",
			"external_provider_selector":                                 "An endpoint decides.",
			"external_provider_selector.endpoint_name":                   "The endpoint's full resource name.",
		}),
	},
	{
		typeName:    "realm_config",
		message:     &corepb.RealmConfig{},
		attribute:   "authwise_realm.config",
		description: "A realm's config: provisioning, recovery, bot protection, SMS and the default locale. The authentication policy is not part of it: declare an `authwise_realm_authentication_policy`.",
		descriptions: map[string]string{
			"default_locale": "The BCP 47 tag the realm writes to its people in when nothing better is known. Unset is no realm default, which is not the same as `en`.",
			"provisioning":   "How an account comes to exist in the realm.",
			"provisioning.password_validators.config": "The validator's settings.",
			"recovery":                     "Whether a person may reset a forgotten password from the login page, and how long the link lives. Absent is off.",
			"bot_protection":               "The challenge an anonymous request must pass before kit sends mail or SMS or creates an account. Absent is off.",
			"bot_protection.mode":          "`OFF` (the default), `ALWAYS`, or `ADAPTIVE`, which kit refuses for now.",
			"bot_protection.site_key":      "The provider's public key, rendered by the login UI.",
			"bot_protection.secret_ref":    "The provider's secret key, for `TURNSTILE` and `HCAPTCHA`, as `{ name = authwise_secret.x.name }`.",
			"bot_protection.endpoint_name": "The endpoint serving AuthwiseBotCheckService, for `ENDPOINT`.",
			"sms":                          "Where the realm will send an SMS, and how many per destination prefix.",
		},
		omit: map[string]bool{authenticationKey: true},
	},
	{
		typeName:    "audience_config",
		message:     &corepb.AudienceConfig{},
		attribute:   "authwise_audience.config",
		description: "An audience's config: its token lifetimes, scopes, CORS and Access settings.",
		descriptions: withTokenLifetimes(map[string]string{
			"scopes.openid": "Whether the audience offers OpenID Connect. Unset means true; set false for an audience that must not.",
			"authorization.access.default_resource_expander_endpoint_name": "The endpoint for pull-mode resource-parent expansion when a resource type names none.",
			"authorization.access.default_subject_expander_endpoint_name":  "The endpoint for pull-mode subject-group expansion.",
			"authorization.access.decision_sampling":                       "The percentage of ALLOW decisions recorded as `access.decision` audit events, 0-100. DENY is always recorded.",
		}),
	},
	{
		typeName:    "endpoint_auth",
		message:     &corepb.EndpointAuth{},
		attribute:   "authwise_endpoint.auth",
		description: "The credential kit presents on every call to an endpoint, resolved at call time so a secret's new version takes effect without touching the endpoint.",
		descriptions: map[string]string{
			"bearer":             "Sends `authorization: Bearer <token>`.",
			"bearer.token":       "The token, as `{ name = authwise_secret.x.name }`.",
			"basic":              "Sends `authorization: Basic …`.",
			"basic.password":     "The password, as `{ name = authwise_secret.x.name }`.",
			"header":             "Sends `<name>: <value>`, an API-key header.",
			"header.name":        "The header name, lower-cased on the wire.",
			"header.value":       "The value, as `{ name = authwise_secret.x.name }`.",
			"kit_token":          "Sends `authorization: Bearer <token>`, where the token is an access token kit signs from `issuer` for each call. No secret is stored.",
			"kit_token.issuer":   "The issuer's full resource name, of this tenant.",
			"kit_token.audience": "The `aud` claim. Required by kit.",
		},
	},
}

func configJSONDataSources() []func() datasource.DataSource {
	out := make([]func() datasource.DataSource, 0, len(configJSONSpecs))
	for _, s := range configJSONSpecs {
		out = append(out, newConfigJSONDataSource(s))
	}
	return out
}
