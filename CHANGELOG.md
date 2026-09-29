## 0.1.0 (Unreleased)

NOTES:

* The provider now builds against `git.authwise.com/authwise/apis` (v0.4.0),
  which is `api-client-go` renamed — the published module stopped being a
  client library when kit's `.proto` sources joined the generated stubs. No
  practitioner-visible change; the package layout is identical.

FEATURES:

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
  attributes come with these changes. The new
  Apple / OIDC / OAuth data sources are #20.
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
