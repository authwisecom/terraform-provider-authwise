## 0.1.0 (Unreleased)

NOTES:

* The provider now builds against `git.authwise.com/authwise/apis` (v0.4.0),
  which is `api-client-go` renamed — the published module stopped being a
  client library when kit's `.proto` sources joined the generated stubs. No
  practitioner-visible change; the package layout is identical.

FEATURES:

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
