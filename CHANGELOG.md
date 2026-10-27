## 0.21.1 (Unreleased)

NOTES:

* **Worked branding examples (#50, docs#24).** `authwise_theme` and
  `authwise_appearance_profile` have examples on their pages, and
  `examples/branding` applies them whole: a logo asset, a theme built
  from the center layout's starter, a default appearance profile and a
  second one a client signs in with. The theme's description says the
  layout is fixed at creation, that colours are six hex digits without
  `#`, and that `content` names an asset at `/assets` plus its `path`.
  No provider behaviour changes.

## 0.21.0 (October 26, 2026)

NOTES:

* **Paired with [apis v0.43.0](https://git.authwise.com/authwise/apis/-/releases/v0.43.0)
  and kit v1.61.0.** kit fills `account_client_id` and honours
  `request_limits` from v1.61.0. An older kit never fills
  `account_client_id`, so it stays null, and ignores `request_limits`,
  keeping its defaults.

FEATURES:

* **`authwise_issuer` and its data sources read back `account_client_id`
  (#34, kit#827).** This is the issuer's "Account Security" client,
  which kit creates at the issuer's first audience; it is output only and
  lives outside `config`, so an issuer that sets `config` does not drift.
  In the apply that creates the first audience the resource still reads
  it as null; read the `authwise_issuer` data source with `depends_on` on
  the audience to use it in that apply, as the issuer page shows.
  `config.account_client_id` remains an override that wins when set.
* **`authwise_provider_magic_link` takes `request_limits` (#49,
  kit#831).** It caps sign-in requests per hour: `per_identifier_per_hour`
  (1 to 20, default 3), `per_ip_per_hour` (1 to 100, default 10) and
  `per_realm_per_hour` (1 to 10000, default 500). Each field is set
  alone; 0 or unset takes kit's default, and there is no unlimited. A
  higher cap lets more mail through, to a person's inbox or to someone
  probing addresses. kit refuses a value out of bounds at apply; the
  provider has no plan-time range check, as with `code_length`.

## 0.20.0 (October 26, 2026)

NOTES:

* **Paired with [apis v0.41.0](https://git.authwise.com/authwise/apis/-/releases/v0.41.0).**
  The two new attributes take effect from kit v1.59.0. An older kit ignores
  them and keeps its defaults, so this release is safe ahead of the kit
  upgrade.
* **A realm people are invited into should set
  `provisioning = { invitations = true }` (kit#816).** A realm created
  through the API has invitations off until its config turns them on.
  From kit v1.59.1 an invite to such a realm is refused at send. The
  passwordless example and the `authwise_provider_magic_link` example now
  set it.

FEATURES:

* **`authwise_provider_magic_link` takes `link_scope` and
  `unknown_address` (#48, kit#817, kit#825).** Leaving them unset keeps
  kit's defaults, and each alternative gives something up.
  `link_scope = "ANY_BROWSER"` lets the link sign in whichever browser
  opens it, once the person confirms. Whoever reads the mailbox can then
  sign in anywhere, so pair it with a step-up rule. It applies only with
  `LINK_AND_CODE`; the default is `SAME_BROWSER`.
  `unknown_address = "TELL"` tells a person their address has no
  account, where the realm would not create one. That reveals which
  addresses have accounts, at the rate the request throttle allows. The
  default is `SILENT`. Both take kit's upper-case spelling, as `mode`
  does.

## 0.19.0 (October 26, 2026)

NOTES:

* **Paired with [kit v1.56.0](https://gitlab.authwise.io/authwise/kit/-/releases/v1.56.0) and [apis v0.40.0](https://git.authwise.com/authwise/apis/-/releases/v0.40.0); kit v1.56.0 or later is required.** kit merged issuer
  domains into domains (kit#808): every domain now has an id (`d-…`), an
  issuer names its host by that id, and a custom host is a domain with an
  issuer. Upgrade kit first. Keep a copy of the state before the first
  apply with 0.19.0 (`terraform state pull`): 0.18 cannot read the state
  0.19.0 writes. The `authwise_domain` page has the upgrade steps.
* **The upgrade was run against a real kit.** 0.18.1 state (a domain, an
  issuer on it, an audience) on kit v1.55.0, kit rolled to v1.56.0, then
  0.19.0 with the recipe's configuration: the plan was empty before and
  after apply. A custom host's waits were tested against the provider's
  fakes only, as that install had no domain controller.

BREAKING CHANGES:

* **`authwise_domain.domain_id` is the domain's id, assigned by Authwise;
  the hostname moves to `hostname` (#47).** Replace `domain_id = "<host>"`
  with `hostname = "<host>"`. Existing state upgrades itself: the refresh
  finds each domain by its hostname and records its id, and the plan
  shows no changes. `config` is removed (it had no settings). Import takes
  `tenants/{t}/domains/{d}`, or the hostname in place of the id.
* **`authwise_issuer.domain_id` replaces `domain_name` as the issuer's
  host (#47).** Set `domain_id = authwise_domain.<name>.domain_id`;
  `domain_name` is now read-only and holds the hostname. Kit fills in the
  id for existing issuers, so the plan shows no changes. If it plans to
  replace an issuer, do not apply: kit is older than v1.56.0.

FEATURES:

* **`authwise_domain` takes a custom host (#47).** With `issuer_id` set it
  is a hostname of yours serving that issuer, with everything
  `authwise_issuer_domain` had: `dns_records`, `state`, the `failure_*`
  attributes, `traffic_target`, `certificate_expires_at`, the wait for the
  DNS records on create and for the hostname's release on destroy, and
  timeouts. It adds `activated_at` and `labels`.
* **New resource `authwise_domain_activation` (#47):**
  `authwise_issuer_domain_activation` for an `authwise_domain`. It takes
  the domain's `name` in `domain`, waits for `PROVISIONING` or `ACTIVE` in
  the same way, and refuses a native host, which has nothing to wait for.

DEPRECATIONS:

* **`authwise_issuer_domain` and `authwise_issuer_domain_activation` (#47).**
  They still work, through kit's deprecated issuer-domains API, and go
  once kit removes it (kit#810). Move with `moved` blocks to
  `authwise_domain` and `authwise_domain_activation` (Terraform 1.8 or
  later): the domain keeps its id and certificate, and the plan shows no
  changes. On older Terraform, `terraform state rm` and import by the same
  id.

## 0.18.1 (October 23, 2026)

NOTES:

* **apis v0.39.0, kit v1.53.1, unchanged from 0.18.0.**

BUG FIXES:

* **`authwise_issuer_domain_activation` no longer waits forever (#46).**
  0.18.0 waited for `ACTIVE`, but a domain reaches `ACTIVE` only once
  Authwise finds the traffic record in public DNS, and that record waits
  on the activation, so the apply timed out. Create now returns at
  `PROVISIONING` or `ACTIVE`: from `PROVISIONING` the certificate is
  issued and attached and the hostname routes, so the traffic record can
  go in with no window of TLS errors. `state` says which it saw; `ACTIVE`
  follows on the controller's next check once the traffic record has
  propagated. A `FAILED` domain still fails the apply, and the timeouts
  are unchanged. An apply that timed out under 0.18.0 saved no
  activation; run it again with 0.18.1.

## 0.18.0 (October 23, 2026)

NOTES:

* **apis v0.39.0, kit v1.53.1, unchanged from 0.17.1.** The new resource
  needs nothing new on the server.

BREAKING CHANGES:

* **`authwise_issuer_domain` drops `wait_for_active`.** It could not finish
  in the apply that creates the challenge record, which needs the domain
  first. Remove it from the configuration and use
  `authwise_issuer_domain_activation` (below). State written by 0.17 needs
  nothing.

FEATURES:

* **New resource `authwise_issuer_domain_activation`:** waits until an
  issuer domain is `ACTIVE`. Put it between the challenge record
  (`depends_on`) and the traffic record (which depends on it), and one
  apply creates the domain and the challenge record, waits for the
  certificate, then points traffic, with no window of TLS errors. It owns
  nothing in Authwise; destroying it only forgets it.

## 0.17.1 (October 22, 2026)

NOTES:

* **kit v1.53.1; apis v0.39.0 unchanged.** The two refusals below come
  from kit v1.53.1; against v1.53.0 nothing changes.
* The `authwise_issuer_domain` doc says how a destroy orders a domain
  before its issuer, which kit v1.53.1 requires (`ISSUER_HAS_DOMAINS`,
  kit#796).

BUG FIXES:

* **`authwise_issuer_domain` names the `DOMAINS_UNAVAILABLE` refusal:** an
  install with no domain controller refuses create (kit#798), and the
  diagnostic now says why. `PLAN_REQUIRED` names the plan as "Business"
  rather than kit's enum spelling.
* **`authwise_issuer` names the `ISSUER_HAS_DOMAINS` refusal:** kit
  refuses to delete an issuer that still has custom domains (kit#796), and
  the destroy diagnostic now names the count and a hostname and says to
  destroy the domains first.

## 0.17.0 (October 21, 2026)

NOTES:

* **apis v0.39.0, kit v1.53.0.** `authwise_issuer_domain` needs kit
  v1.53.0 or later; the other resources need nothing new.

FEATURES:

* **New resource `authwise_issuer_domain` (#44):** a hostname of yours,
  such as `login.acme.com`, that serves an issuer, with the certificate
  obtained and renewed by Authwise. Create returns once the computed
  `dns_records` (one `traffic` and one `challenge` record) are known, so
  DNS resources keyed on `purpose` take them in the same apply.
  `wait_for_active` waits for the certificate; destroy waits until the
  hostname is released. `traffic_target`, `state`, `failure_code`,
  `failure_message`, `failure_record` and `certificate_expires_at` read
  back. Create's
  refusals name their reason (`HOSTNAME_CLAIMED`, `HOSTNAME_APEX`,
  `HOSTNAME_IS_ISSUER`, `PLAN_REQUIRED`). Needs kit v1.53.0.

## 0.16.3 (October 20, 2026)

NOTES:

* **tfinfra v0.0.20; apis v0.34.0, guard-control v0.17.0, kit v1.47.0.**
  No new server requirement. Parent identifiers
  (`tenant_id`, `realm_id`, `issuer_id`, `audience_id`, `network_id`, …)
  now read back from the resource's name, and are filled into existing
  state on the next refresh. A parent the configuration omits shows the
  provider default instead of null. Removing a configured parent from the
  configuration, or changing the provider default, keeps the resource where
  it is rather than replacing it. Workspaces that worked around #43 with
  `lifecycle { ignore_changes = [tenant_id] }` (or `[realm_id]`) can drop
  the block.

BUG FIXES:

* **An import no longer forces replacement (#43):** importing a resource
  whose configuration sets its parent, such as `authwise_guard_network`
  with `tenant_id` or `authwise_factor` with `realm_id`, left the parent
  null in state. The first plan then replaced the resource
  (`+ tenant_id = "gt-…" # forces replacement`). The parents now land in
  state on import and on every read, and the plan after an import is
  empty. Changing a configured parent still replaces the resource
  (tfinfra v0.0.20).

## 0.16.2 (October 18, 2026)

NOTES:

* **tfinfra v0.0.19; apis v0.34.0, guard-control v0.17.0, kit v1.47.0.**
  No new server requirement. State written by 0.16.0 or earlier shows the
  whitespace diff below once more. Applying it sends nothing to the
  server, because the update mask leaves out a config that compares equal,
  and the next plan is clean.

BUG FIXES:

* **Whitespace-only diffs on `config` after upgrading:** the config data
  sources' `any` (and every Any- or Struct-typed attribute read from the
  server) came from protojson, which varies its whitespace on purpose,
  seeded from a hash of the provider binary. Each build could spell the
  same value differently: 0.14.0 to 0.16.0 put a space after the commas,
  and 0.16.1 did not. Terraform compares the configured string with state
  as written, so `config = data.authwise_*_config.x.any` planned
  `jsonencode( # whitespace changes )` after an upgrade. The encoding is
  now compact in every build (tfinfra v0.0.19).
* **A list or map written empty reads back empty:** `labels = {}` or an
  empty list was read back as null (tfinfra v0.0.17).

## 0.16.1 (October 18, 2026)

NOTES:

* **apis v0.34.0, guard-control v0.17.0, kit v1.47.0.** No new server
  requirement: the same releases as 0.16.0.

BUG FIXES:

* **`authwise_secret` plans without the material when it isn't sent:** a
  null `payload_wo` with `payload_wo_version` set failed every plan with
  "payload_wo_version without payload_wo", even when the plan would send
  nothing. A workspace that fed `payload_wo` from an ephemeral variable,
  set only to create or rotate, could not plan without the secret on disk.
  The check now runs at plan against state: `payload_wo` is required only
  when creating an inline secret or when `payload_wo_version` changes, and
  may be null otherwise.

## 0.16.0 (October 14, 2026)

NOTES:

* **apis v0.34.0, guard-control v0.17.0, kit v1.47.0.** A role other than
  `guardcontrol.user`, a grant at a network, and a lookup by slug need
  guard-control v0.17.0 (guard-control#57, #59). Against an older
  guard-control, these fail with Unimplemented. A central administrator's
  credential writes role grants with its own bearer. A users-link home
  administrator's credential is written as the tenant's product principal,
  which needs a kit release carrying kit#776 (none yet; v1.47.0 does not).
  Existing enrolment grants keep the enrolment calls and need nothing
  newer: their state gains `role = "guardcontrol.user"` on refresh,
  without replacing them. The rest of the provider still works with
  guard-control v0.16.0 and kit v1.46.0.

FEATURES:

* **`authwise_guard_enrolment_grant` gains `role` and `network_id` (#42):**
  `role` is `guardcontrol.user` (the default: may enrol),
  `guardcontrol.tenantAdmin`, `guardcontrol.networkAdmin` or
  `guardcontrol.viewer`, and is written through guard-control's `GrantRole`.
  A role is bound at the Guard tenant, or at the network `network_id`
  names. `networkAdmin` requires a network and `tenantAdmin` refuses one;
  both are checked at plan. The administrative roles carry the home's
  step-up condition, which is reported as `condition_id`. Import still
  takes `<gt-id>/<binding>`, and the role and network are read back.
* **`authwise_guard_tenant` by `slug` (#42):** the data source takes `slug`
  or `name`, exactly one. A slug is resolved through guard-control's
  unauthenticated `ResolveTenant`, then the tenant is read by name as
  before, so configurations need not hard-code a `gt-…`. The slug is only an
  alias: pass `guard_tenant_id` to other resources.
* **apis v0.34.0 fields:** Guard tenants report `slug`, `product_client_id`,
  and `users.administers`, `users.console_client_id` and
  `users.step_up_condition_id`. `authwise_guard_network` takes an optional
  `slug`.

## 0.15.0 (October 14, 2026)

NOTES:

* **apis v0.33.0, guard-control v0.16.0, kit v1.46.0.** The two new Guard
  resources and the data source need guard-control v0.16.0; against an older
  guard-control every call fails Unimplemented. An enrolment grant is
  written on the users-link home with the provider's own bearer, which
  must administer the users-link tenant: a tenant administrator's
  credential is admitted by a home on kit v1.46.0 or later (kit#770). On
  an older home, apply with an operator credential or one holding
  `access.bindingAdmin` at the tenant. The deprovisioning account needs a
  guard-control install with `sweep.sealKeyFile` in remote access mode.
  The rest of the provider still works with kit v1.45.0 and guard-control
  v0.15.0.

FEATURES:

* **`authwise_guard_enrolment_grant` resource (#41):** lets one user or
  group of a Guard tenant's users-link tenant enrol devices: a
  `guardcontrol.user` binding at the Guard tenant, written through
  guard-control's `GrantEnrolment`, which fixes the role and the resource.
  Every argument replaces the grant, and destroying it revokes the
  binding. It is the same row an `authwise_access_binding` with that role
  writes, so declare a grant one way only. Import by
  `<gt-id>/<binding>`; a grant anchored at a network is refused.
* **`authwise_guard_deprovisioning` resource and data source (#41):** the
  service account the deprovisioning sweep re-checks the users-link home
  with. `client_secret_wo` is write-only, as `authwise_secret.payload_wo` is:
  sent on every change and bumped with `client_secret_wo_version`, never
  stored. guard-control tests the credential before storing it, and
  destroying the resource clears it. The data source reads the last
  sweep's result (`last_result`, `last_message`, `last_revoked`). Import by
  `gt-…`.

IMPROVEMENTS:

* Guard resources now run their own config validation; the wrapper that
  refuses a Guard resource without `guard_endpoint` used to hide it.

## 0.14.0 (October 14, 2026)

NOTES:

* **apis v0.31.0, api-client-support v0.4.1, kit v1.45.0, guard-control
  v0.15.0.** `authwise_tenant_config` needs kit v1.45.0, the first kit
  release in which `tenant.admin` carries `tenancy.tenants.update` (kit#769).
  On an estate that was already running, the `tenant.admin` role at central
  also needs that permission written to it in Access. Without it, under
  client-credentials auth, every write the resource makes is refused with
  PermissionDenied. The data source makes no API call. The rest of the
  provider still works with kit v1.43.0.

FEATURES:

* **`authwise_tenant_config` resource and data source (#40):** manage an
  existing tenant's `TenantConfig` (its brand, `branding.logo_asset_id` and
  `branding.logo_dark_asset_id`, and the tenant-level token lifetimes and
  CORS) and optionally its `labels` through `PatchTenant`, with a mask over
  only those. The tenant's display name, owner and classification are left
  alone, and the resource never creates or deletes a tenant. `config` is
  authoritative. Destroy clears the config, and `labels` when set, so the
  logo assets can be deleted after it: name the logo as
  `authwise_asset_content.x.asset_id`, since kit refuses deleting an asset,
  or removing its content, while the brand names it. The data source
  builds the config from typed attributes, as `authwise_issuer_config`
  does. Import by `t-…` or `tenants/t-…`.

## 0.13.1 (October 14, 2026)

NOTES:

* **api-client-support v0.4.1; apis v0.31.0, kit v1.43.0, guard-control
  v0.15.0.** No new server requirement.

BUG FIXES:

* **`authwise_asset_content` failed `Unauthenticated` (#39):** the
  provider attached the bearer to unary calls only, so `UploadAsset` and
  `DownloadAsset`, the asset content's two streaming calls, reached kit
  without one. Every create, update and refresh of the resource failed,
  while `authwise_asset` worked with the same credentials. Streaming calls
  now carry the bearer too. The acceptance stack now refuses any call,
  unary or streaming, that has no bearer, so a missing token fails the
  tests the way it fails against kit.
* The move from api-client-support v0.2.1 to v0.4.1 brings v0.3.0's
  renewal of the local `awctl auth login` through the refresh grant: the
  login is renewed rather than expiring after the access token's lifetime.
  Client-credentials auth is unchanged.


## 0.13.0 (October 13, 2026)

NOTES:

* **apis v0.31.0, kit v1.43.0, guard-control v0.15.0.** No new server
  requirement: the config data sources make no API call, and the configs
  they build are the ones kit already accepts. As in 0.12.0, service
  accounts need kit v1.43.0 and the rest works with kit v1.38.0.

FEATURES:

* **Typed config data sources (#38):** `authwise_issuer_config`,
  `authwise_realm_config`, `authwise_audience_config` and
  `authwise_endpoint_auth` build `authwise_issuer.config`,
  `authwise_realm.config`, `authwise_audience.config` and
  `authwise_endpoint.auth` from typed attributes, and expose the protojson
  as `json`. The attribute names are the message's fields in snake_case and
  are checked at plan, along with enum values, durations (Go strings such
  as `30m`) and the oneofs: an issuer takes at most one provider selector,
  an endpoint at most one auth scheme. Zero values are left out of `json`,
  as kit leaves them out of what it returns. They make no API calls, and
  the schemas come from the apis messages, so a field apis adds appears
  without a provider change. `authwise_realm_config` has no
  `authentication`, which belongs to `authwise_realm_authentication_policy`.
  `jsonencode` still works.
* The identifier-first, endpoint and authn examples, and the issuer,
  endpoint and provider docs, now use the data sources.

## 0.12.0 (October 12, 2026)

NOTES:

* **apis v0.31.0, kit v1.43.0, guard-control v0.15.0.** Built against apis
  v0.31.0. kit v1.43.0 is the first kit release that serves service accounts
  (kit#756) and their roles (kit#763), at central. The rest of the provider
  still works with kit v1.38.0.

FEATURES:

* **`authwise_service_account.roles` (#37, kit#763):** the roles the account
  holds at its tenant, by key. A service account may hold `tenant.admin`,
  `tenant.viewer` and `guardcontrol.admin`, and kit refuses any other; the
  refusal is shown on `roles`. When set, `roles` is authoritative: the
  account holds exactly these, it changes in place, and an empty set unbinds
  them all. Unset, the provider leaves the account's roles alone and reads
  them back. A tenant administrator can now give the account a role, which
  0.11.0 left to an operator.
* Destroying an account unbinds its `roles` first. kit still refuses while
  any other binding names it.
* The provider doc's "CI: a service account" now sets `roles` instead of
  having an operator write the binding.

## 0.11.0 (October 12, 2026)

NOTES:

* **apis v0.30.0, guard-control v0.15.0, kit v1.38.0.** Built against apis
  v0.30.0, which carries kit#756's service accounts. Every other resource
  still pairs with kit v1.38.0. The two service account resources need the
  first kit release after v1.40.1 that carries kit#756, served at central.

FEATURES:

* **`authwise_service_account` (#37):** a machine principal the tenant
  owns, for CI. It is a client of the admin issuer, created through the
  public tenancy service with a member seat at the tenant and no roles.
  `display_name` and `expires_at` replace it when changed, since nothing
  changes an account in place. It exposes `client_id`, `status` and its live
  `secrets` (id, created, expiry, never the plaintext), and imports by its
  name (`tenants/{t}/service-accounts/{c}`). kit refuses to delete it while
  a binding names it, and the error says to remove the binding first.
* **`authwise_service_account_secret` (#37):** the account's secret, minted
  once and kept in state as a sensitive attribute, like
  `authwise_client_secret`. It also returns the `token_url` and `audience`
  to use it with. Replace it to rotate (`keepers`): with `rotate` (the
  default) the other live secrets keep working for `grace` (24 hours
  unless set) and then lapse. Destroying it does not revoke the secret,
  because the tenancy service has no per-secret revoke. A secret that has
  expired or lapsed drops out of state, and the next apply mints another.
* **`authwise_client.owning_tenant_id`**, read-only: set on a service
  account's client.
* The provider doc's Authentication section gains "CI: a service account".
  Until kit#763 adds `roles`, binding a role to the account needs an
  operator: a tenant administrator holds no `access.bindings.create` in the
  estate audience.

## 0.10.0 (October 10, 2026)

NOTES:

* **apis v0.28.0, guard-control v0.15.0, kit v1.38.0.** Built against apis
  v0.28.0, whose only change to the provider's surface is guard-control's
  (guard-control#53). kit's surfaces are unchanged, so the provider still
  pairs with kit v1.38.0. The Guard resources need guard-control v0.15.0.

BREAKING CHANGES:

* **`authwise_guard_tenant` is gone (#36).** Under E22 (estate tenancy), a
  Guard tenant is created only through guard-control's mTLS tenancy
  listener, by awtenant and the portal worker. apis v0.28.0 removes
  `CreateTenant`, `DeleteTenant` and `ListTenantParents` from
  `GuardTenantAdminService`, so no bearer surface creates or deletes one.
  The same already holds for an Authwise tenant.
  * The Guard tenant's id (`gt-…`) now comes from awtenant. Set it as
    `tenant_id` on the Guard resources and data sources, the way the
    provider already references an Authwise tenant it does not create.
  * The `authwise_guard_tenant` and `authwise_guard_tenants` data sources
    stay and read a tenant by its name (`tenants/gt-…`) or list the ones
    the credential can see. Their `users` gains `tenant_id`.
  * To upgrade, remove the tenant from state (`terraform state rm
    authwise_guard_tenant.<name>`) and replace references to
    `authwise_guard_tenant.<name>.guard_tenant_id` with the id. A state
    that still holds one fails to plan, since the provider no longer has
    the type. Do not destroy it under 0.9.0 first: that deletes the tenant.

## 0.9.0 (October 9, 2026)

NOTES:

* **apis v0.24.0, guard-control v0.12.0, kit v1.38.0.** No dependency
  changes since 0.8.0. The provider still pairs with kit v1.38.0; the
  guard-catalog example below needs kit v1.40.0.
* **`examples/guard-catalog` is guard-control v0.12.0's.** Its 41
  permissions are regenerated from that version's `authz-permissions`:
  `tenants.register` and `tenants.unregister` are gone, `tenants.create`,
  `tenants.delete` and `tenants.list` are back, and `events.get`,
  `events.list` and `events.search` are new. It declares
  `guardcontrol.tenant` (under `tenant`) and `guardcontrol.network` (under
  `guardcontrol.tenant`), both PUSH. The role also carries kit's
  `access.resourceLinks.create`, `.delete` and `.list`, which are read from
  the catalog rather than created. The example needs kit v1.40.0 (E21,
  kit#738).

FEATURES:

* **`authwise_access_resource_type`**, with its singular and plural data
  sources: a resource type declared in an audience's Access catalog, with
  its `parent_type` and `expansion_mode` (`NONE`, `PUSH`, `PULL` or
  `PATH`). It is keyed by its name, like a permission, and every other field
  changes in place.

## 0.8.0 (October 9, 2026)

NOTES:

* **apis v0.24.0, guard-control v0.12.0.** Built against apis v0.24.0. kit's
  surfaces the provider uses are unchanged, so kit v1.38.0 is still the kit
  release this pairs with. The Guard resources need guard-control v0.12.0,
  and do not work against earlier versions.

BREAKING CHANGES:

* **Guard tenants are Guard's own (#35).** Built against apis v0.24.0, whose
  guard-control (v0.12.0, guard-control#41) removed `RegisterTenant` and
  `UnregisterTenant`. A Guard tenant is now an entity of Guard's (`gt-…`)
  created under a parent kit tenant through `GuardTenantAdminService`.
  Against guard-control v0.12.0, 0.7.0's `authwise_guard_tenant` fails with
  Unimplemented.
  * `authwise_guard_tenant` takes `parent_tenant_id` (the kit tenant) and
    `users` (`issuer`, `audience`, `access_endpoint`: where the tenant's
    people sign in and are decided). Both are set at create only, so
    changing either replaces the tenant. `guard_tenant_id` is now assigned
    by guard-control and read-only. `created_by` is read back. Destroying it
    deletes the tenant, and guard-control's refusal while the tenant has
    networks or users is shown as it is.
  * The other Guard resources and the Guard list data sources now require
    `tenant_id`, and it must be a Guard tenant's id
    (`authwise_guard_tenant.<name>.guard_tenant_id`). They no longer inherit
    the provider's `tenant_id`, which is a kit tenant's.
  * A tenant registered under 0.7.0 has no equivalent. Remove it from state
    and create the Guard tenant again, or import one created elsewhere by
    its name (`tenants/gt-…`).

FEATURES:

* `authwise_guard_node` and `authwise_guard_relay` read back
  `last_seen_at`, which is read-only.

## 0.7.0 (October 7, 2026)

NOTES:

* **apis v0.22.0, guard-control v0.8.1.** Built against apis v0.22.0, which
  adds Guard's admin API and changes nothing else; kit v1.38.0 is still the
  kit release this pairs with. The Guard resources need guard-control
  v0.8.1.
* **The Registry groups the pages (#31).** Every resource and data source
  page now carries one of five subcategories: Identity, Sign-in providers,
  Factors, Access and Guard. `make docs` fills them in after tfplugindocs,
  from `provider.Subcategory`, and a unit test fails when a registered type
  has none.

FEATURES:

* **Guard (#32).** The provider manages Guard, the mesh VPN, over
  guard-control's API:
  * `authwise_guard_tenant` registers a kit tenant with Guard, by its AWID
    (`guard_tenant_id`). Destroying it unregisters the tenant, which
    guard-control refuses while the tenant has networks or users.
  * `authwise_guard_network`: a mesh in the tenant. Leave `cidr` out for the
    whole of 100.64.0.0/10. Changing it replaces the network.
  * `authwise_guard_relay`: `url`, `ca_cert_pem`, `region` and `priority`.
  * `authwise_guard_resource`: a `subnet`, `host` or `application` and its
    `address`. `serving_node_ids` is read-only. The serving nodes are an
    authoritative set of node names in `authwise_guard_resource_nodes`.
  * `authwise_guard_node`: a server's node, registered ahead of time.
    Without a `public_key` it is `PENDING`, and its one-time `auth_code`
    (sensitive, kept in state like an invite's code) enrols the server.
    With one it is `ACTIVE` at once. `address` is allocated when left out
    and changing it replaces the node. Enrolment writes `public_key` and
    `endpoint`, which read back without a diff. `state`, `roles`,
    `allowed_ips`, `owner_id` and `auth_code_expires_at` are read-only.
    `:issueAuthCode` and `:revoke` are not modelled. People's nodes join by
    invite and are not terraform's.
  * `authwise_guard_node_grants`: what a node may reach, the resources and
    nodes of its network, as an authoritative set. A grant an invite gave
    the node is removed unless listed.
  * `authwise_guard_invite` is create-only: changing anything replaces it.
    `code` and `url` are shown once and kept in state, sensitive. A spent or
    expired invite reads back without a diff. Replace it
    (`-replace=authwise_guard_invite.x`) to issue another. An imported
    invite has neither.

  Each has a singular and a plural data source. The provider's new
  `guard_endpoint` (or `AUTHWISE_GUARD_ENDPOINT`) is guard-control's gRPC
  address. The same bearer goes to it as to `endpoint`. Unset, a Guard
  resource fails at plan and a Guard data source at read. On Guard
  resources, `network_id` is required: there is no provider default for
  it. A Guard tenant's id attribute is `guard_tenant_id`, and a network's
  `guard_network_id`.
* **`examples/guard`** builds a network from the tenant down to a gateway
  server serving the office subnet, and an invite.
  The acceptance suite applies it in one configuration with
  `examples/guard-catalog`.

FIXES:

* **`examples/guard-catalog` carries guard-control v0.8.1's 37
  permissions.** It had 31 from an older release: the six `clients.*`
  permissions that no longer exist, and none for relays, invites,
  `networks.enrol`, `nodes.associateGrants` or `nodes.issueAuthCode`. A
  role built from it could not manage relays or invites.

## 0.6.0 (October 7, 2026)

NOTES:

* **kit v1.38.0.** This release pairs with kit v1.38.0 and is built against
  apis v0.21.0. It needs kit v1.38.0: an older kit still expects the single
  `grant_type`, and the client fields below do not exist there. Within that,
  recovery came in kit v1.34.0 and bot protection and the SMS policy in
  v1.36.0. kit v1.38.0 is a reset release for clients (see its CHANGELOG),
  so clients are recreated rather than migrated in place.
* **The Omni rename (kit#223) changes nothing here.** apis renames
  `CLASSIFICATION_CUSTOMER` to `CLASSIFICATION_OMNI`, but classification is
  a tenant field and the provider has no tenant resource or data source.

BREAKING CHANGES:

* **`authwise_client.grant_type` is replaced by `grant_types` (apis
  v0.21.0, kit#404).** The same goes for the `authwise_client` and
  `authwise_clients` data sources. `grant_types` is a list of RFC 7591
  values:
  * `["authorization_code", "refresh_token"]` for an application that
    refreshes. A kit v1.38.0 server issues a refresh token only to a client
    that lists `refresh_token`, so `"authorization_code"` alone no longer
    implies it.
  * `["client_credentials"]` for a service.
  * `["saml_idp"]` for a SAML relying party. These were
    `grant_type = "authorization_code"` before, as in `examples/saml`.

  A configuration that sets `grant_type` fails at plan. Rewrite it, as the
  examples now do.
* **A public client holds no secret.** An application's
  `token_endpoint_auth_method` defaults to `none`, and kit refuses an
  `authwise_client_secret` for a `none` client. An application that holds
  a secret must set `token_endpoint_auth_method` to `client_secret_basic`
  or `client_secret_post`. A service defaults to `client_secret_basic`, so
  a `client_credentials` client with a secret needs no change beyond
  `grant_types`.

FEATURES:

* **`authwise_client` says what a client may do (apis v0.21.0, kit#404).**
  New optional, computed attributes, also on both client data sources:
  * `kind`: `CLIENT_KIND_APPLICATION`, `CLIENT_KIND_SERVICE` or
    `CLIENT_KIND_AGENT`. kit derives it from `grant_types` when omitted:
    `client_credentials` alone is a service, anything else an application.
  * `token_endpoint_auth_method`: `client_secret_basic`,
    `client_secret_post` or `none`. kit derives it from `kind` when omitted.
    `private_key_jwt` is refused until kit#411.
  * `status`: `CLIENT_STATUS_ACTIVE` (the default), `CLIENT_STATUS_DISABLED`
    or `CLIENT_STATUS_QUARANTINED`. A client that is not active is refused
    at the token endpoint, and its tokens introspect inactive.
  * `expires_at`: an RFC 3339 timestamp. Once it is reached, the client is
    refused as if disabled.

  What kit derives lands in state and is kept, so a later change to
  `grant_types` does not re-derive `kind` or the method. If you move a
  client between an application and a service, set them explicitly.
* **`authwise_realm` config accepts `recovery` (apis v0.19.0).**
  Self-service password reset: `selfServiceReset`, `resetTtl` (5 minutes to
  24 hours, written in seconds such as `"1800s"`; omit it for kit's one-hour
  default) and `supportContact`.
* **`authwise_realm` config accepts `botProtection` and `sms` (apis
  v0.20.0).** `botProtection` takes `mode` (`OFF` or `ALWAYS`), `provider`
  (`TURNSTILE`, `HCAPTCHA` or `ENDPOINT`), `siteKey`, `secretRef`
  (`{ name = authwise_secret.x.name }`, which also orders the realm after
  the secret), `endpointName`, `failMode`, `timeout` and
  `refuseDisposableSignup`. `sms` takes `allowedRegions`, `deniedRegions`
  and `sendsPerPrefixPerHour`. kit refuses `ADAPTIVE`, `ALWAYS` with no
  provider, a vendor provider without both keys, and a region that is not
  ISO 3166-1 alpha-2. As with the rest of the JSON config, leave zero values
  such as `failMode = "CLOSED"` out: kit omits them on read, and an explicit
  one plans a diff on every run.

## 0.5.0 (October 4, 2026)

NOTES:

* **kit v1.32.0.** This release pairs with kit v1.32.0 and is built against
  apis v0.18.0. It needs kit v1.32.0: against an older kit, `is_default`
  and an issuer's appearance do not work, and `trust_upstream_email_verified`
  needs kit v1.30.0.

BREAKING CHANGES:

* **`authwise_user`: `email`, `email_verified`, `phone_number` and
  `phone_number_verified` are read-only (kit#662, kit#666).** kit derives
  them from the account's proven identifiers and refuses them on every user
  write, whatever the value. A configuration that sets one now fails at
  plan. Remove them, and give a user an address through an invitation
  (`InviteUser`). The provider no longer sends them on create or update, so
  updating a user who has an address works.
* **`authwise_issuer.appearance_profile_id` is removed (kit#680).** An
  issuer's appearance is now whichever of its appearance profiles is the
  default. To migrate, delete `appearance_profile_id` from the issuer and
  set `is_default = true` on the profile it named. A client's or audience's
  `appearance_profile_id` is unchanged and still overrides the issuer's
  default.

FEATURES:

* **`authwise_appearance_profile` gains `is_default` (kit#680).** An issuer
  with any profiles always has exactly one default, and kit keeps it that
  way:
  * Its first profile becomes the default without asking, and reads back
    `true`.
  * Set `is_default = true` on the one profile that should be the default,
    and leave it out on the others. `is_default = false` is refused at plan,
    because kit cannot honour it.
  * To move the default, set it on the new profile and remove it from the
    old one, in one apply. The provider moves it with kit's `:makeDefault`
    rather than writing the flag, which kit refuses.
  * kit refuses to delete the default while the issuer has other profiles.
    When they are all destroyed together, the provider retries the
    default's delete for up to a minute while the others go. It fails after
    that if the others remain, for example if the default is the only one
    removed from configuration, or if the default depends on another
    profile, which Terraform then destroys later. With `-parallelism=1`, a
    full destroy can also time out if Terraform deletes the default first.

* **`authwise_provider` gains `trust_upstream_email_verified` (kit#663).**
  It is an optional bool and defaults to false. It makes kit count the
  upstream's verified email as its own proof of the address. kit refuses
  it on a type with no upstream: `usernamePassword`, `magicLink`,
  `smsCode`, `passkey` and `dummy`. The `authwise_provider` and
  `authwise_providers` data sources read it.
* **`authwise_user` gains `credentials_changed_at` (apis v0.15.0).** It is
  read-only, and the `authwise_user` and `authwise_users` data sources read
  it too.

## 0.4.0 (September 30, 2026)

NOTES:

* **kit v1.25.0.** This release pairs with kit v1.25.0 and is built
  against apis v0.13.0. kit v1.25.0 is the first release that carries
  kit#633, which `link_by_verified_email` needs.

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
