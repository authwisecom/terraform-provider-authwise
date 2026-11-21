package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- authwise_provider: the trust settings, described (tf#82) and checked (tf#83) ---

const (
	signInProviderTypeName  = "authwise_provider"
	signInProvidersTypeName = "authwise_providers"
	signInProvidersListAttr = "providers"

	attrProviderType               = "provider_type"
	attrTrustUpstreamEmailVerified = "trust_upstream_email_verified"
	attrLinkByVerifiedEmail        = "link_by_verified_email"
	attrTrustedEmailDomains        = "trusted_email_domains"
	attrUpstreamAcrMap             = "upstream_acr_map"

	providerTypeSAML = "saml"

	// upstreamAcrAttributePrefix starts the attribute form of an
	// upstream_acr_map key (kit pkg/lib/model/upstream_acr.go).
	upstreamAcrAttributePrefix = "attribute:"

	// entraAcrKey is the key that maps Entra ID's MFA statement over SAML.
	entraAcrKey = "attribute:http://schemas.microsoft.com/claims/authnmethodsreferences=http://schemas.microsoft.com/claims/multipleauthn"
)

// notFederatedProviderTypes have no upstream: kit refuses the email trust
// settings and trusted_email_domains on them (kit pkg/lib/model).
var notFederatedProviderTypes = map[string]bool{"usernamePassword": true, "magicLink": true, "passkey": true}

// trustedEmailDomainPattern is kit's one form for a trusted_email_domains
// entry (kit pkg/lib/model/trusted_email_domains.go, kit#1011): a lowercase
// DNS name of two or more labels, each 1 to 63 characters of a-z, 0-9 and
// inner hyphens, at most 253 characters in all. Nothing is folded.
var trustedEmailDomainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// signInProviderDescriptions say what each of a provider's settings does,
// its default, the types it applies to and what turning it on costs,
// rewritten from the comments on Provider in kit's core.proto (apis
// v0.86.0). The generator carries no field descriptions, so the resource
// and both data sources take them from here.
var signInProviderDescriptions = map[string]string{
	attrProviderType: "The kind of provider, which must match `config`'s type. kit knows these: " +
		"`usernamePassword`, `magicLink` and `passkey`, which sign people in with Authwise itself; " +
		"and the federated types, which sign them in at an upstream identity provider: " +
		"`google`, `microsoft`, `apple`, `facebook`, `github`, `linkedin`, `okta`, `auth0`, `authwise`, " +
		"`oidc` (any OpenID Connect provider), `oauth` (OAuth 2.0 with no id_token) and `saml`.",

	"status": "`active` (the default) or `disabled`. A disabled provider still appears in lists but is offered nowhere: " +
		"sign-in and routing skip it (a routing target that names it falls back to the realm's own choice), " +
		"and nothing can be connected to it. The people who sign in through it keep their accounts and sign-in methods, " +
		"and can use it again once it is `active`. kit refuses to disable the realm's last active provider. " +
		"Changing it is made in place. Removing the line keeps the current status: set `active` to turn a provider back on.",

	attrTrustUpstreamEmailVerified: "Whether an address this upstream states verified counts as proven by Authwise. " +
		"Default `false`: the upstream's statement is kept on the person's link and read only by the sign-in link offer " +
		"(`link_by_verified_email`). It never makes the address a recovery route.\n\n" +
		"With `true`, each sign-in through this provider that states the address verified records it as one of the person's proven addresses. " +
		"**A proven address is a recovery route**: whoever controls that address at the upstream can recover the account, " +
		"and sign in to it where the realm offers a magic link. " +
		"Set it only for an identity provider that is the directory of record for its people's addresses, such as your workforce IdP. " +
		"Do not set it for a social provider, or for a partner whose directory holds addresses that belong to someone else, such as guest accounts. " +
		"The trust is in this provider alone, not the realm's others.\n\n" +
		"What \"states verified\" means depends on the type. An OpenID Connect or OAuth upstream says it with `email_verified`, " +
		"limited to `trusted_email_domains` when that is set. A SAML assertion has no way to say it, so on a `saml` provider " +
		"an address counts as stated verified when its domain is in `trusted_email_domains`, and this setting is refused at plan " +
		"without that list.\n\n" +
		"Federated types only: kit refuses it on `usernamePassword`, `magicLink` and `passkey`.",

	attrLinkByVerifiedEmail: "Whether to offer to connect this provider to an existing account at sign-in. Default `false`.\n\n" +
		"With `true`, when a person signs in through this provider with no account connected to it yet, " +
		"the upstream states their address verified, and exactly one active account in the realm holds that address proven and has a password, " +
		"kit offers to connect the two. The person must first sign in to that account with its password; nothing is connected silently. " +
		"Without it, such a sign-in goes the way the realm's settings for new federated users say: a new account, or a refusal.\n\n" +
		"The cost: it trusts this upstream's statement that the person controls the address. " +
		"An upstream that marks addresses verified without proving them lets someone start the offer for an account that is not theirs, " +
		"though they still need its password. The trust is in this provider alone, as with `trust_upstream_email_verified`, " +
		"and the two are independent: neither needs the other.\n\n" +
		"On a `saml` provider an address counts as stated verified when its domain is in `trusted_email_domains`, " +
		"and this setting is refused at plan without that list.\n\n" +
		"Federated types only: kit refuses it on `usernamePassword`, `magicLink` and `passkey`.",

	attrTrustedEmailDomains: "The email domains this upstream is the directory of record for, such as `[\"contoso.com\"]`. " +
		"An address's domain must match an entry exactly: `contoso.com` does not cover `eu.contoso.com`.\n\n" +
		"On a `saml` provider the list is how an address comes to count as verified: a SAML assertion has no way to say so, " +
		"so an address whose domain is listed counts as stated verified, and no other does. " +
		"`link_by_verified_email` and `trust_upstream_email_verified` then read that statement, and each is refused at plan on a `saml` provider without the list. " +
		"The list with both settings off is accepted and does nothing. " +
		"On the other federated types it is optional and narrows which addresses the upstream's own `email_verified` counts for; empty means no narrowing.\n\n" +
		"The list, not the setting alone, is the guard: an Entra ID tenant holds guest accounts whose addresses belong to someone else. " +
		"List only the domains the directory owns.\n\n" +
		"Each entry is a lowercase domain name of at least two labels. A wildcard, an `@`, upper case, a trailing dot or a repeat is refused at plan. " +
		"Federated types only: refused on `usernamePassword`, `magicLink` and `passkey`.",

	"trust_upstream_amr": "Whether the upstream's statement of how the person signed in counts toward the realm's authentication rule. " +
		"Default `false`: a sign-in through a federated provider counts as one factor whatever the upstream says, " +
		"because kit checked the upstream's signature, not its methods. The token's `amr` relays what the upstream said either way.\n\n" +
		"With `true`, the upstream's RFC 8176 `amr` values count: `mfa` as two factors, `hwk` and `swk` as phishing-resistant. " +
		"So do the entries of `upstream_acr_map` that match. " +
		"A realm that requires two factors then does not ask for a second one after an upstream that already did MFA. " +
		"The cost: kit cannot check the claim, so an upstream that says `mfa` without doing it satisfies the realm's rule. " +
		"Set it for an upstream whose MFA you rely on. The trust is in this provider alone, not the realm's others.\n\n" +
		"Federated types, `saml` included; it has no effect on `usernamePassword`, `magicLink` and `passkey`.",

	attrUpstreamAcrMap: "Maps the upstream's statement of how the person signed in to the name of one of the realm's acr levels. " +
		"Read only when `trust_upstream_amr` is `true`, since it is the same trust in what the upstream says. Default empty.\n\n" +
		"A key takes one of two forms, matched exactly and case-sensitively:\n" +
		"  - a bare value, compared with the id_token's `acr` (OpenID Connect) or the assertion's AuthnContextClassRef (SAML);\n" +
		"  - `attribute:<name>=<value>`, which matches when the named SAML attribute or id_token claim carries that value among its values. " +
		"The name runs to the first `=`.\n\n" +
		"A matched entry counts as its level's requirement met by the upstream; of several, the strongest wins. " +
		"An empty or malformed key, or an entry with no level, is refused at plan.\n\n" +
		"Entra ID over SAML states MFA in an attribute, not in the class reference, so its entry is " +
		"`\"" + entraAcrKey + "\" = \"urn:authwise:acr:2\"`, " +
		"with the value the name of the realm's two-factor level.",
}

// signInProviderKitNotes are the kit release each setting's behaviour
// needs, appended to its description. When kit changes a setting again,
// only its line here changes.
var signInProviderKitNotes = map[string]string{
	attrTrustedEmailDomains: "**Needs kit 1.98.0 or later** (kit#1011). An earlier kit does not store it.",
	attrTrustUpstreamEmailVerified: "**On a `saml` provider it needs kit 1.98.0 or later** (kit#1011). " +
		"Through kit 1.97.x it had no effect there.",
	attrLinkByVerifiedEmail: "**On a `saml` provider it needs kit 1.98.0 or later** (kit#1011). " +
		"Through kit 1.97.x it had no effect there.",
	"trust_upstream_amr": "**On a `saml` provider it needs kit 1.98.0 or later** (kit#1010). " +
		"Through kit 1.97.x a SAML sign-in counted as one factor whether it was set or not.",
	attrUpstreamAcrMap: "**Needs kit 1.98.0 or later** (kit#1010). Through kit 1.97.x kit stored it and nothing read it.",
}

// signInProviderDescription is attribute's description, its kit note
// included, or "" for an attribute this file does not describe. Each
// paragraph after the first is indented, so that in the generated docs it
// stays inside the attribute's list item.
func signInProviderDescription(attribute string) string {
	d := signInProviderDescriptions[attribute]
	if l := signInProviderKitNotes[attribute]; l != "" {
		d += "\n\n" + l
	}
	return strings.ReplaceAll(d, "\n\n", "\n\n  ")
}

// describeResourceAttributes sets the description on each attribute this
// file describes, and the plan-time checks on the two whose entries kit
// refuses in a malformed form.
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
			if name == attrUpstreamAcrMap {
				a.Validators = append(a.Validators,
					mapvalidator.KeysAre(upstreamAcrKeyValidator{}),
					mapvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)))
			}
			attrs[name] = a
		case schema.ListAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			if name == attrTrustedEmailDomains {
				a.Validators = append(a.Validators,
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(
						stringvalidator.LengthAtMost(253),
						stringvalidator.RegexMatches(trustedEmailDomainPattern,
							"a lowercase domain name such as \"contoso.com\", with no wildcard, \"@\" or trailing dot")))
			}
			attrs[name] = a
		}
	}
}

// describeDataSourceAttributes is describeResourceAttributes for a data
// source's schema, without the checks.
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
		case dsschema.ListAttribute:
			a.MarkdownDescription = signInProviderDescription(name)
			attrs[name] = a
		}
	}
}

// upstreamAcrKeyValidator is kit's ParseUpstreamAcrKey at plan: a key is a
// non-empty bare value, or `attribute:<name>=<value>` with both parts
// non-empty. Nothing is trimmed or folded.
type upstreamAcrKeyValidator struct{}

var _ validator.String = upstreamAcrKeyValidator{}

func (upstreamAcrKeyValidator) Description(context.Context) string {
	return "a bare value, or attribute:<name>=<value> with both parts non-empty"
}

func (v upstreamAcrKeyValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (upstreamAcrKeyValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if msg := upstreamAcrKeyProblem(req.ConfigValue.ValueString()); msg != "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid upstream_acr_map key", msg)
	}
}

// upstreamAcrKeyProblem says what is wrong with key, or "" when it is one
// of the two forms.
func upstreamAcrKeyProblem(key string) string {
	if key == "" {
		return "An upstream_acr_map key is empty."
	}
	rest, ok := strings.CutPrefix(key, upstreamAcrAttributePrefix)
	if !ok {
		return ""
	}
	name, value, found := strings.Cut(rest, "=")
	if !found || name == "" || value == "" {
		return fmt.Sprintf("Key %q: write %q followed by <name>=<value>, both non-empty, or a bare value.", key, upstreamAcrAttributePrefix)
	}
	return ""
}

// providerTrustConfig is the part of a provider's configuration the
// trust checks read.
type providerTrustConfig struct {
	providerType     types.String
	trustEmail, link types.Bool
	domains          types.List
}

// validateProviderTrust refuses at plan what kit refuses on write
// (kit#1011): trusted_email_domains on a type with no upstream, and on a
// saml provider either email setting without the list. An unknown value
// decides nothing; kit has the last word at apply.
func validateProviderTrust(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) {
	var c providerTrustConfig
	diags.Append(config.GetAttribute(ctx, path.Root(attrProviderType), &c.providerType)...)
	diags.Append(config.GetAttribute(ctx, path.Root(attrTrustUpstreamEmailVerified), &c.trustEmail)...)
	diags.Append(config.GetAttribute(ctx, path.Root(attrLinkByVerifiedEmail), &c.link)...)
	diags.Append(config.GetAttribute(ctx, path.Root(attrTrustedEmailDomains), &c.domains)...)
	if diags.HasError() || c.providerType.IsUnknown() || c.providerType.IsNull() || c.domains.IsUnknown() {
		return
	}
	t := c.providerType.ValueString()
	listed := len(c.domains.Elements()) > 0
	if listed && notFederatedProviderTypes[t] {
		diags.AddAttributeError(path.Root(attrTrustedEmailDomains), "trusted_email_domains is for a federated provider",
			fmt.Sprintf("A %s provider has no upstream to trust. Remove trusted_email_domains.", t))
	}
	if t == providerTypeSAML && !listed {
		refuseUnlisted(diags, attrTrustUpstreamEmailVerified, c.trustEmail)
		refuseUnlisted(diags, attrLinkByVerifiedEmail, c.link)
	}
}

// refuseUnlisted refuses an email setting turned on for a saml provider
// with no trusted_email_domains.
func refuseUnlisted(diags *diag.Diagnostics, name string, value types.Bool) {
	if !value.ValueBool() {
		return
	}
	diags.AddAttributeError(path.Root(name), name+" on a saml provider needs trusted_email_domains",
		"A SAML assertion cannot say an address is verified, so trusted_email_domains is that statement: "+
			"list the domains this directory is the record for, or set "+name+" to false.")
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
	validateProviderTrust(ctx, req.Config, &resp.Diagnostics)
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
