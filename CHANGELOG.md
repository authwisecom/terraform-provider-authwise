## 0.1.0 (Unreleased)

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

* `authwise_domain` and `authwise_scope` gain required `domain_id` /
  `scope_id` attributes. kit keys both by a caller-supplied name, which the
  provider had no way to send — it created rows under an empty key — so
  neither resource could be used before this release.
