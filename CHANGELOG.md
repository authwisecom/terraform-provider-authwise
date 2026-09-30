## 0.4.0 (September 30, 2026)

NOTES:

* **apis v0.13.0.** This release is built against apis v0.13.0 (from kit
  6f63ec1e). `link_by_verified_email` needs a kit release that carries
  kit#633.

FEATURES:

* **`authwise_provider` gains `link_by_verified_email` (#29, kit#633).**
  It is an optional bool and defaults to false. It lets a person who signs
  in through this upstream with a verified email link to the account that
  already holds that email, once they confirm with the account's password.
  kit refuses it on a type with no upstream: `usernamePassword`,
  `magicLink`, `smsCode`, `passkey` and `dummy`. The `authwise_provider` and
  `authwise_providers` data sources read it.

## 0.3.0 (September 30, 2026)

NOTES:

* **kit v1.24.0.** This release pairs with kit v1.24.0 and is built against
  apis v0.12.0, the version 0.2.0 used. kit v1.24.0 is the first release
  that deletes an asset's file (kit#54). Its object-storage installs also
  need platform v1.13.0, or platform-hosted v0.9.0 on hosted installs.
  Against an older kit or platform, uploads and refreshes work, but
  destroying an `authwise_asset_content` fails.

FEATURES:

* **New resource `authwise_asset_content` (#28).** The file behind an
  `authwise_asset`. `authwise_asset` manages only the asset's metadata, and
  until now the provider had no way to upload the bytes the hosted pages
  serve at its `path`. Set `source` (a local file) or `content_base64`.
  `content_sha256` tracks the content: editing the file plans an in-place
  update that uploads it again, and a file changed on the server shows as
  drift. Destroying it removes the file and leaves the asset.

## 0.2.0 (September 29, 2026)

NOTES:

* **kit v1.23.1.** This release is built against apis v0.12.0, the apis
  version kit v1.23.1 serves. Use v1.23.1 rather than v1.23.0, whose images
  were published incomplete. kit v1.22.x has no in-place upgrade to
  v1.23.x: drop the store and let bootstrap recreate it.
* **kit#620 refuses an out-of-scope reference at apply.** A bare-id `*_id`
  field naming a row outside the writer's scope is refused on create and
  update as `InvalidArgument`: `<field>: no <noun> <id> in scope`, the same
  whether the row is missing or belongs to another issuer or tenant. The
  fields are a client's `audience_id` and `appearance_profile_id`, an
  audience's or issuer's `appearance_profile_id`, the issuer config's
  `account_client_id`, an appearance profile's `theme_id`, the SAML
  certificate ids, and a binding's `condition_id`. A configuration that
  applied against v1.22.x with a dead reference (a literal id of an
  audience that does not exist, say) now fails there instead.
FEATURES:

* **Ids and references (tfinfra v0.0.16, #25, #26).** Additive: nothing is
  renamed, and a split written before still works.
  * Every resource, singular data source and plural data source item now
    exports its own id as `<type>_id`, the last segment of `name`:
    `audience_id` on `authwise_audience`, `realm_id` on `authwise_realm`,
    `client_id` on `authwise_client`, `client_secret_id` on
    `authwise_client_secret`, and so on. It is computed, and a refresh
    fills it into existing state. On the caller-named resources
    (`authwise_domain`, `authwise_scope`, `authwise_access_permission`,
    `authwise_access_role`) it is the required input it already was.
  * A `*_id` attribute takes another resource's `<type>_id`, so a
    reference no longer splits `name`:

    ```terraform
    # before
    audience_id = element(split("/", authwise_audience.api.name), 5)

    # now
    audience_id = authwise_audience.api.audience_id
    ```

    A `*_ref = { name = … }` block and the association resources still take
    `name`. The README states the rule.
  * Reference attributes are checked by id prefix in plan: the parent
    attributes (`tenant_id` `t-`, `issuer_id` `i-`, `realm_id` `r-`,
    `audience_id` `a-`, `client_id` `c-`, on every resource, plural data
    source and the provider block), a client's `audience_id` and
    `appearance_profile_id` (`ap-`), an audience's and issuer's
    `appearance_profile_id`, an appearance profile's `theme_id` (`th-`) and
    a binding's `condition_id` (`axc-`). A full name pasted into one fails
    in plan, and the error names the attribute to reference instead. This
    is the plan-time half of kit#620. The SAML configs'
    `*_certificate_id` fields are not checked, since kit also takes a
    certificate's `name` there, but the examples now pass
    `certificate_id`.
  * Every `element(split(...))` in the examples, docs and README now reads
    the attribute.

* **`authwise_client_secret` (apis v0.12.0, kit#617, #18).** A client's
  secret, minted through `MintClientSecret`. kit returns the secret exactly
  once, as `<id>_<plaintext>`, so the sensitive `secret` attribute is kept
  in state and carried across every refresh. Treat the state as sensitive.
  `expires_at` changes in place and the secret keeps working. kit offers no
  rotate verb, so rotate by changing a value in `keepers`, which replaces
  the resource; with `create_before_destroy` the successor is minted before
  the old secret is deleted. `terraform import` cannot recover the secret.
  `authwise_client_secrets` lists a client's secrets, without the secret;
  there is no singular data source.

  ```terraform
  resource "authwise_client_secret" "ci" {
    client_id = authwise_client.ci.client_id
    keepers   = { rotation = "2026-09" }

    lifecycle {
      create_before_destroy = true
    }
  }
  ```

## 0.1.0 (September 29, 2026)

NOTES:

* The provider now builds against `git.authwise.com/authwise/apis` (v0.4.0),
  which is `api-client-go` renamed — the published module stopped being a
  client library when kit's `.proto` sources joined the generated stubs. No
  practitioner-visible change; the package layout is identical.

FEATURES:

* **Plural data sources.** Every resource now has one, for example
  `authwise_realms`, `authwise_providers` and `authwise_access_permissions`.
  Each lists every object under one parent: the parent's `tenant_id` /
  `issuer_id` / `realm_id` / `audience_id` are optional and fall back to
  the provider defaults. The objects arrive as a list carrying the same
  attributes as the singular data source, caller-chosen ids included, and
  every page of the API's results is read. There is no server-side filter
  yet; filter with a `for` expression:

  ```terraform
  data "authwise_providers" "all" {
    realm_id = "r-01"
  }

  locals {
    saml = [for p in data.authwise_providers.all.providers : p.name if p.provider_type == "saml"]
  }
  ```

* **Social and enterprise sign-in (apis v0.8.0, kit#587, #20).**
  * New config data sources:
    * `authwise_provider_apple`: Sign in with Apple. The `.p8` key is an
      `authwise_secret` named by `private_key_ref`.
    * `authwise_provider_oidc`: discovery-driven OpenID Connect from
      `issuer`, for Okta, Auth0, Entra, Keycloak, Ping and another
      Authwise, with `endpoints` to pin what discovery does not give.
    * `authwise_provider_oauth`: OAuth 2.0 without an id_token. It reads a
      userinfo document through a claim map, and both `identifier_source`
      and `claim_map` are required.
  * The seven reshaped data sources take the new fields; see BREAKING
    CHANGES.
  * `examples/social` has Google, Microsoft, GitHub, Apple, Okta (as
    `oidc`) and Discord (as `oauth`) on one realm. Each client secret is
    held by reference. The acceptance suite applies it and checks that no
    secret material reaches a provider row or state.

* **Identifier-first login (apis v0.11.0, #24).** Additive. The issuer's
  `multiRealmProviderSelector` is now a routing selector. The login page
  asks for an identifier, and ordered rules match it by domain or CEL
  condition and route it to a realm, optionally to one provider in it.
  Everything else goes to a required `defaultTarget`. It is written through
  `authwise_issuer.config` with `jsonencode`.
  * `docs/resources/issuer.md` documents the selector, what kit refuses,
    and how to write the JSON: lowerCamel keys, zero values left out (omit
    `kind` for EMAIL), and the selector written whole.
  * `examples/identifier-first` has a consumer realm as the default and a
    partner domain routed to a SAML realm. The acceptance suite applies it.
  * kit refuses a selector without `defaultTarget`, which is new in apis
    v0.11.0. No such selector could have worked before.

* **Passwordless primaries (kit#194, kit#195, #21).** Additive; both
  provider types are live in every shipped kit profile.
  * `authwise_provider_magic_link` builds a `magicLink` provider's config:
    `code_length` (6–8), `ttl` (a duration, `"10m"`), `mode`
    (`LINK_AND_CODE` or `CODE_ONLY`) and `identifier_attribute` (`email`
    only). Every field has kit's default when unset.
  * `authwise_provider_passkey` gives a `passkey` provider its `any`
    envelope. The config has no fields. The provider signs nobody in
    without an enabled `webauthn` factor on the realm, and the data
    source's description says so.
  * `examples/passwordless` has a password-or-magic-link realm and a
    passkey realm with its webauthn factor. The acceptance suite applies it.
  * Config data source inputs now describe how to write a value whose type
    does not say, such as a duration or a JSON document (tfinfra v0.0.12).

* **Endpoints describe their TLS, their deadline and a kit-signed token
  (kit#603, #22).** Requires `apis` v0.10.0 and kit >= 1.22.0. Additive: an endpoint written
  before this reads the same.
  * `authwise_endpoint` gains `tls`, a typed nested attribute:
    `ca_pem`, `server_name`, `insecure_skip_verify` and
    `client_certificate`, which names an `authwise_certificate` holding a
    private key and presents it for mTLS.
  * `authwise_endpoint` gains `timeout`, the provider's first duration
    attribute. It is a string in Go duration syntax (`"5s"`, `"1.5s"`,
    `"500ms"`, `"1m"`) and keeps the spelling written. An import reads the
    protojson form (`"60s"`). Needs tfinfra's Duration support.
  * `auth` takes the new `kitToken = { issuer, audience }` scheme: kit signs
    a token from the named issuer, and no secret is stored anywhere.
  * `docs/resources/endpoint.md` documents each `auth` scheme with
    `jsonencode`, including the lowerCamel keys (`kitToken`). It also
    covers `insecure`, which means plaintext, applies to gRPC only, and is
    refused on REST.
  * With `apis` v0.10.0 (kit v1.22.0), kit refuses to delete an endpoint
    while anything names it, and refuses to delete a certificate or issuer
    while an endpoint uses it. The error names the holder. The docs cover
    destroy ordering: a reference, even inside `jsonencode`, orders the
    destroy, and a literal name string needs `depends_on`. The acceptance
    stub now enforces the same refusals and serves `:check` and
    `:referrers`.
  * `endpoint_type = "REST"` can be written explicitly. REST is the
    enum's zero value, and until tfinfra v0.0.13 an explicit zero read back
    as null and failed the apply. Any enum's explicit zero now reads back as
    written, and so does a duration's spelling inside a nested attribute
    such as `tls`.
  * `examples/endpoints/{bearer,kit_token,mtls}` are applied by the
    acceptance suite. The example harness now copies an example's
    `file()` inputs along with its `main.tf`.

* **Authn + secrets (kit#544, #19).** Requires `apis` v0.7.0 and a kit
  built from `develop` (no kit release carries these RPCs yet).
  * `authwise_factor` — a second step the realm offers
    (`tenants/{t}/realms/{r}/factors/{f}`), with `factor_type` fixed at
    creation and `status` `active` / `disabled`. Disabling is not deleting:
    enrolled authenticators survive a disable, not a destroy. Its per-type
    `config` is built by the new `authwise_factor_totp`,
    `authwise_factor_webauthn`, `authwise_factor_duo` and
    `authwise_factor_external` config data sources.
  * `authwise_realm_authentication_policy` — the realm's rules (CEL), floor,
    enrollment, remembered devices, session, throttle, risk and acr levels,
    managed apart from `authwise_realm` so the two never overwrite each
    other.
  * `authwise_secret` rewritten for kit's sealed secrets: the material goes
    in through the write-only `payload_wo` (Terraform >= 1.11) and is never
    stored in state; bumping `payload_wo_version` rotates it through
    `:addVersion`. `external = { store, key }` selects an operator store
    (refused by kit until kit#372). kit refuses to delete a referenced
    secret, and the provider says how to fix it.
  * **kit's warnings reach the apply** (kit#586). kit accepts some writes
    it doubts — a rule requiring a factor type no active factor offers —
    and says so in an `authwise-warning` response header. Realm, factor and
    policy writes now show those as Terraform warnings instead of leaving
    them in kit's log.
  * `authwise_realm_authentication_context_schema` data source — the CEL
    variables a rule may read, the factor types the kit build runs, and the
    realm's current policy warnings, for a `check` block.
  * `examples/authn` — factors, the Duo secret by reference, the policy and
    the warnings check, applied by the acceptance suite.
  * `authwise_scope_access_permissions` — a scope's access-permission set,
    over the new `AssociateAccessPermissionsToScope` pair.
  * `authwise_endpoint` gains `auth` (bearer, basic or header, each naming
    a secret), as a protojson document.
  * Picked up from `apis` v0.6.0 along the way: `display_name` on
    `authwise_access_role`, `layout` on `authwise_theme`.
* **New resources:** `authwise_access_permission`, `authwise_access_role`,
  `authwise_access_role_access_permissions`, `authwise_access_condition`,
  `authwise_access_binding` — the audience-scoped Access catalog. Requires
  kit >= 1.10.0, whose role→permission listing is audience-scoped (kit#312).
* **New data sources:** the singular reads for the four Access entities.
* `authwise_access_binding` resolves `role_name` in the binding's own
  audience before writing: the API accepts a binding naming a role that does
  not exist there and it silently grants nothing (kit#296).
* `examples/guard-catalog` — guard-control's prerequisite catalog end to end
  (31 permissions, the role and its associations, a root-anchored binding,
  the console audience and OIDC client), applied by the acceptance suite.

BREAKING CHANGES:

* **Access bindings are unique per grant (kit#616).** Against a kit with
  kit#616, two `authwise_access_binding` resources that declare the same
  grant fail on apply with `ALREADY_EXISTS`, even if they differ only in
  `expires_at`. A grant is its audience, subject, role, resource and
  condition. Changing `expires_at` alone updates the binding in place, so
  extending a grant never collides with itself.
* **The social provider configs take `apis` v0.8.0's shapes**, which the
  bump to v0.9.0 brings in. No kit install could hold a working row of
  these types, so what breaks is configuration, not stored data:
  * `authwise_provider_dropbox` is removed. Dropbox is not an identity
    provider, and kit refuses the type.
  * `scope` becomes a `scopes` list on `authwise_provider_google`,
    `_microsoft`, `_github`, `_facebook`, `_okta`, `_auth0` and
    `_linkedin`.
  * The parameters kit only forwarded move to `authorization_params`:
    Google's `prompt`, Microsoft's `prompt_style` (send it as `prompt`),
    and GitHub's `allow_signup`, `prompt` and `display`.
  * Okta's and Auth0's `tenant_url` becomes `issuer`.
  * Facebook's `user_fields` becomes the `fields` list.
  * LinkedIn's `include_granted_scopes` is removed.

  The new `claim_map`, `authorization_params` and `identifier_claim`
  attributes come with these changes.
* **The identity role surface is gone** (`apis` v0.6.0 removed its RPCs;
  roles and permissions live in the Access catalog now):
  `authwise_role`, `authwise_permission`, `authwise_user_roles`,
  `authwise_client_roles`, `authwise_role_permissions` and
  `authwise_scope_permissions` are removed, with the `authwise_role` and
  `authwise_permission` data sources. Use `authwise_access_role`,
  `authwise_access_permission` and `authwise_access_role_access_permissions`,
  and `authwise_scope_access_permissions` for a scope's grants. Remove the
  old resources from state with `terraform state rm` — the API that could
  delete them no longer exists.
* **Client secrets are references.** The social provider config data
  sources (`authwise_provider_google`, `_microsoft`, `_github`, `_linkedin`,
  `_facebook`, `_dropbox`, `_okta`, `_auth0`) replace `client_secret` with
  `client_secret_ref = { name = authwise_secret.x.name }` (kit#370): kit no
  longer stores or returns the plaintext.
* **`authwise_secret`'s `value` and `encoding` are removed** — kit removed
  both fields (kit#369). Move the material to `payload_wo`. Existing
  secrets keep their material; importing one does not recover it, since
  nothing returns it.
* **`authwise_realm` refuses an `authentication` key in `config`**; declare
  an `authwise_realm_authentication_policy` instead.

* The `logging` attribute is removed from `authwise_interactive_client_config`.
  It carried a `LoggingConfig` JSON document — a log level plus three flags —
  that kit accepted, validated, persisted and echoed back, and that nothing in
  kit ever read. Log level is set by the server's `LOG_LEVEL` environment
  variable; there was never a path from this attribute to it. Remove it from
  your configuration: it has never had an effect, so nothing about the running
  system changes when you do. kit removed the field in `e6269682` and reserved
  both its field number and its JSON name, so state and stored configuration
  still carrying the key are read back unchanged.

* `authwise_domain` and `authwise_scope` gain required `domain_id` /
  `scope_id` attributes. kit keys both by a caller-supplied name, which the
  provider had no way to send — it created rows under an empty key — so
  neither resource could be used before this release.
* `authwise_appearance_profile` is issuer-scoped, matching kit
  (`tenants/*/issuers/*/appearance-profiles/*`): it gains an `issuer_id`
  attribute, its parent gains the issuer segment, and its import IDs use
  the `appearance-profiles` collection rather than `appearanceProfiles`.
  Import was the only operation the old collection name reached, and it
  rejected every real name.
