## 0.1.0 (Unreleased)

NOTES:

* The provider now builds against `git.authwise.com/authwise/apis` (v0.4.0),
  which is `api-client-go` renamed — the published module stopped being a
  client library when kit's `.proto` sources joined the generated stubs. No
  practitioner-visible change; the package layout is identical.

FEATURES:

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
