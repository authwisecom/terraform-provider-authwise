package provider

import (
	"context"
	"strings"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// --- authwise_provider: the trust settings, described (tf#82) ---

const (
	signInProviderTypeName  = "authwise_provider"
	signInProvidersTypeName = "authwise_providers"
	signInProvidersListAttr = "providers"
)

// signInProviderDescriptions say what each of a provider's settings does,
// its default, the types it applies to and what turning it on costs,
// rewritten from the comments on Provider in kit's core.proto (v1.97.4).
// The generator carries no field descriptions, so the resource and both
// data sources take them from here.
var signInProviderDescriptions = map[string]string{
	"provider_type": "The kind of provider, which must match `config`'s type. kit 1.97 knows these: " +
		"`usernamePassword`, `magicLink` and `passkey`, which sign people in with Authwise itself; " +
		"and the federated types, which sign them in at an upstream identity provider: " +
		"`google`, `microsoft`, `apple`, `facebook`, `github`, `linkedin`, `okta`, `auth0`, `authwise`, " +
		"`oidc` (any OpenID Connect provider), `oauth` (OAuth 2.0 with no id_token) and `saml`.",

	"status": "`active` (the default) or `disabled`. A disabled provider still appears in lists but is offered nowhere: " +
		"sign-in and routing skip it (a routing target that names it falls back to the realm's own choice), " +
		"and nothing can be connected to it. The people who sign in through it keep their accounts and sign-in methods, " +
		"and can use it again once it is `active`. kit refuses to disable the realm's last active provider. " +
		"Changing it is made in place. Removing the line keeps the current status: set `active` to turn a provider back on.",

	"trust_upstream_email_verified": "Whether an address this upstream states verified counts as proven by Authwise. " +
		"Default `false`: the upstream's `email_verified` is kept on the person's link and read only by the sign-in link offer " +
		"(`link_by_verified_email`). It never makes the address a recovery route.\n\n" +
		"With `true`, each sign-in through this provider that states the address verified records it as one of the person's proven addresses. " +
		"**A proven address is a recovery route**: whoever controls that address at the upstream can recover the account, " +
		"and sign in to it where the realm offers a magic link. " +
		"Set it only for an identity provider that is the directory of record for its people's addresses, such as your workforce IdP. " +
		"Do not set it for a social provider, or for a partner whose directory holds addresses that belong to someone else, such as guest accounts. " +
		"The trust is in this provider alone, not the realm's others.\n\n" +
		"Federated types only: kit refuses it on `usernamePassword`, `magicLink` and `passkey`.",

	"link_by_verified_email": "Whether to offer to connect this provider to an existing account at sign-in. Default `false`.\n\n" +
		"With `true`, when a person signs in through this provider with no account connected to it yet, " +
		"the upstream states their address verified, and exactly one active account in the realm holds that address proven and has a password, " +
		"kit offers to connect the two. The person must first sign in to that account with its password; nothing is connected silently. " +
		"Without it, such a sign-in goes the way the realm's settings for new federated users say: a new account, or a refusal.\n\n" +
		"The cost: it trusts this upstream's statement that the person controls the address. " +
		"An upstream that marks addresses verified without proving them lets someone start the offer for an account that is not theirs, " +
		"though they still need its password. The trust is in this provider alone, as with `trust_upstream_email_verified`.\n\n" +
		"Federated types only: kit refuses it on `usernamePassword`, `magicLink` and `passkey`.",

	"trust_upstream_amr": "Whether the sign-in methods this upstream reports (`amr`) count toward the realm's authentication rule. " +
		"Default `false`: a sign-in through a federated provider counts as one factor whatever the upstream says, " +
		"because kit checked the upstream's signature, not its methods. The token's `amr` relays what the upstream said either way.\n\n" +
		"With `true`, the upstream's RFC 8176 values count: `mfa` as two factors, `hwk` and `swk` as phishing-resistant. " +
		"A realm that requires two factors then does not ask for a second one after an upstream that already did MFA. " +
		"The cost: kit cannot check the claim, so an upstream that says `mfa` without doing it satisfies the realm's rule. " +
		"Set it for an upstream whose MFA you rely on. The trust is in this provider alone, not the realm's others.\n\n" +
		"Federated types; it has no effect on `usernamePassword`, `magicLink` and `passkey`.",

	"upstream_acr_map": "Maps an upstream `acr` value to the name of one of the realm's acr levels, " +
		"for an upstream that states a context class rather than methods. " +
		"It is meant to count only when `trust_upstream_amr` is `true`, since it is the same trust in what the upstream says. Default empty.",
}

// signInProviderLimits are what a setting does not yet do on the kit
// releases the provider supports. Each is appended to its description, so
// when kit ships a fix only its line here changes.
var signInProviderLimits = map[string]string{
	"trust_upstream_email_verified": "**Through kit 1.97.x, it has no effect on a `saml` provider.** " +
		"A SAML assertion has no standard statement that an address is verified, so there is nothing to trust (kit#1011).",
	"link_by_verified_email": "**Through kit 1.97.x, it has no effect on a `saml` provider**, for the reason " +
		"`trust_upstream_email_verified` has none: a SAML upstream never states an address verified (kit#1011).",
	"trust_upstream_amr": "**Through kit 1.97.x, it has no effect on a `saml` provider:** " +
		"a SAML sign-in counts as one factor whether it is set or not (kit#1010).",
	"upstream_acr_map": "**Through kit 1.97.x, it has no effect on any provider type:** kit stores it and nothing reads it (kit#1010). " +
		"A map set now takes effect, with no change here, on the kit release that reads it.",
}

// signInProviderDescription is attribute's description, its limit
// included, or "" for an attribute this file does not describe. Each
// paragraph after the first is indented, so that in the generated docs it
// stays inside the attribute's list item.
func signInProviderDescription(attribute string) string {
	d := signInProviderDescriptions[attribute]
	if l := signInProviderLimits[attribute]; l != "" {
		d += "\n\n" + l
	}
	return strings.ReplaceAll(d, "\n\n", "\n\n  ")
}

// describeResourceAttributes sets the description on each attribute this
// file describes.
func describeResourceAttributes(attrs map[string]schema.Attribute) {
	for name := range signInProviderDescriptions {
		switch a := attrs[name].(type) {
		case schema.StringAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		case schema.BoolAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		case schema.MapAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		}
	}
}

// describeDataSourceAttributes is describeResourceAttributes for a data
// source's schema.
func describeDataSourceAttributes(attrs map[string]dsschema.Attribute) {
	for name := range signInProviderDescriptions {
		switch a := attrs[name].(type) {
		case dsschema.StringAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		case dsschema.BoolAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		case dsschema.MapAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		}
	}
}

// signInProviderResource is the generated provider resource with its
// settings described. Every other method forwards, the optional interfaces
// included.
type signInProviderResource struct {
	inner resource.Resource
}

var (
	_ resource.ResourceWithConfigure      = &signInProviderResource{}
	_ resource.ResourceWithValidateConfig = &signInProviderResource{}
	_ resource.ResourceWithModifyPlan     = &signInProviderResource{}
	_ resource.ResourceWithImportState    = &signInProviderResource{}
)

func newSignInProviderResource() resource.Resource {
	return &signInProviderResource{inner: generated.NewProviderResource()}
}

func (r *signInProviderResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *signInProviderResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	describeResourceAttributes(resp.Schema.Attributes)
}

func (r *signInProviderResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *signInProviderResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if inner, ok := r.inner.(resource.ResourceWithValidateConfig); ok {
		inner.ValidateConfig(ctx, req, resp)
	}
}

func (r *signInProviderResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if inner, ok := r.inner.(resource.ResourceWithModifyPlan); ok {
		inner.ModifyPlan(ctx, req, resp)
	}
}

func (r *signInProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
}

func (r *signInProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *signInProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
}

func (r *signInProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *signInProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
		return
	}
	resp.Diagnostics.AddError("import not supported", "this resource cannot be imported")
}

// describeSignInProviderDataSources wraps the authwise_provider and
// authwise_providers data sources so they carry the resource's
// descriptions; every other factory passes through.
func describeSignInProviderDataSources(ctx context.Context, factories []func() datasource.DataSource) []func() datasource.DataSource {

	out := make([]func() datasource.DataSource, 0, len(factories))

	for _, factory := range factories {
		resp := &datasource.MetadataResponse{}
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, resp)
		if resp.TypeName == signInProviderTypeName || resp.TypeName == signInProvidersTypeName {
			inner := factory
			factory = func() datasource.DataSource { return &signInProviderDataSource{inner: inner()} }
		}
		out = append(out, factory)
	}

	return out
}

// signInProviderDataSource describes the settings on the singular data
// source's attributes and on the list's nested ones.
type signInProviderDataSource struct {
	inner datasource.DataSource
}

var _ datasource.DataSourceWithConfigure = &signInProviderDataSource{}

func (d *signInProviderDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	d.inner.Metadata(ctx, req, resp)
}

func (d *signInProviderDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	d.inner.Schema(ctx, req, resp)
	describeDataSourceAttributes(resp.Schema.Attributes)
	if list, ok := resp.Schema.Attributes[signInProvidersListAttr].(dsschema.ListNestedAttribute); ok {
		describeDataSourceAttributes(list.NestedObject.Attributes)
		resp.Schema.Attributes[signInProvidersListAttr] = list
	}
}

func (d *signInProviderDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if inner, ok := d.inner.(datasource.DataSourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (d *signInProviderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	d.inner.Read(ctx, req, resp)
}
