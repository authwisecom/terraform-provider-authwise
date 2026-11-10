---
page_title: "Authwise Provider"
description: |-
  Manage Authwise identity and access configuration: tenants' issuers, realms, providers, clients, the Access catalog, secrets and endpoints; and Guard networks.
---

# Authwise Provider

The Authwise provider manages an Authwise install's identity and access
configuration over its published gRPC API. That covers issuers and their
login routing, realms and the providers and factors people sign in with,
clients and audiences, the Access catalog, and the secrets and endpoints
these depend on. It also manages Guard, Authwise's mesh VPN: a tenant's settings,
networks, relays, resources and relay pools, over guard-control's API.

The resources and data sources are grouped by what they manage:

* **Identity**: tenants' issuers, realms, clients, audiences, scopes, users,
  secrets, certificates, endpoints and appearance.
* **Sign-in providers**: `authwise_provider` and the data sources that build
  each provider type's config.
* **Factors**: `authwise_factor` and the data sources that build each factor
  type's config.
* **Access**: an audience's Access catalog of permissions, roles, conditions
  and bindings.
* **Guard**: the `authwise_guard_*` resources.

## Example Usage

```terraform
provider "authwise" {
  endpoint = "api.example.authwise.com:443" # or AUTHWISE_ENDPOINT

  # Scope defaults: a resource inherits these unless it sets its own
  # tenant_id / issuer_id / realm_id / audience_id.
  tenant_id = "t-01"
  issuer_id = "i-01"

  # Client credentials, for CI and automation; each falls back to its
  # AUTHWISE_* environment variable. Omit all of them to use the local
  # `awctl auth login` credential store instead.
  token_url     = "https://auth.example.authwise.com/oauth/token"
  client_id     = "c-terraform"
  client_secret = var.authwise_client_secret
}

variable "authwise_client_secret" {
  type      = string
  sensitive = true
}
```

## Authentication

The provider authenticates with the OAuth 2.0 `client_credentials` grant:
`token_url`, `client_id`, `client_secret` and optionally `audience`. Each
falls back to an environment variable, so CI can configure the provider
without any of them in the configuration:

| attribute | environment variable |
| --- | --- |
| `endpoint` | `AUTHWISE_ENDPOINT` |
| `token_url` | `AUTHWISE_TOKEN_URL` |
| `client_id` | `AUTHWISE_CLIENT_ID` |
| `client_secret` | `AUTHWISE_CLIENT_SECRET` |
| `audience` | `AUTHWISE_AUDIENCE` |
| `guard_endpoint` | `AUTHWISE_GUARD_ENDPOINT` |

With no client credentials at all, the provider uses the local `awctl auth
login` credential store instead, which suits interactive use.

### CI: a service account

A pipeline's credential is a service account: a machine principal the
tenant owns, so its access is the roles bound to it and its actions are
audited as its own `c-…`, not as a person's. A tenant administrator creates
one with `authwise_service_account`, gives it `roles = ["tenant.admin"]`,
and creates a secret with `authwise_service_account_secret`. The pipeline
then sets `AUTHWISE_TOKEN_URL`, `AUTHWISE_CLIENT_ID`,
`AUTHWISE_CLIENT_SECRET` and `AUTHWISE_AUDIENCE="a-01"`. The secret's
`token_url` and `audience` attributes give the first and last of these.

A service account may hold `tenant.admin`, `tenant.viewer` and
`guardcontrol.admin`, at its own tenant. Service accounts, `roles`
included, are served at central only, from kit v1.43.0.

```terraform
# A service account for CI: a machine principal the tenant owns. Its roles
# are what it may do at the tenant; the tenant's administrators set them,
# from tenant.admin, tenant.viewer and guardcontrol.admin. Service accounts
# are served at central only.
resource "authwise_service_account" "ci" {
  display_name = "terraform"
  roles        = ["tenant.admin"]
}
```

The credential's permissions bound what Terraform can do. Setting a secret
reference, for example, needs `identity.secrets.use`.

### A home service account

A tenant with home administration (`authwise_tenant_home_administration`)
can also run Terraform with a **home service account**: a service client of
the tenant's own issuer, created by one of the tenant's own administrators
in the console's Access page, with no Authwise owner involved (kit#939). Its
token comes from the tenant's issuer, for the tenant's administration
audience, and it reaches exactly what a home administrator with the same
roles reaches:

| attribute | value |
| --- | --- |
| `token_url` | the tenant's issuer's token endpoint, `<issuer URL>/oauth/token` |
| `client_id`, `client_secret` | the account's, from the console (the secret is shown once) |
| `audience` | the administration audience's id (`a-…`), from the issuer's `/.well-known/authwise-configuration` |
| `endpoint` | the install's admin API, as for a central account |
| `tenant_id` | the tenant: a home account reaches only its own |

Its roles are bindings in the administration audience at the tenant
(`administration.admin`, `administration.identity`, `administration.viewer`
or a narrower role of the tenant's), with no condition: the step-up condition
tests a person's sign-in, which a client never has.

**What it reaches.** Identity providers, SAML, audiences, clients and their
secrets, certificates, secrets, endpoints, realms, factors, users, groups,
themes, assets, message templates, domains and their issuer bindings, and
access bindings in the tenant's own audiences, Guard's administrative roles
among them. Realms and their people are reached through the tenant's own
sign-in issuer. A home account manages a realm that issuer signs people in
to, or one no issuer signs in to yet, such as a realm it has just created.
A realm behind another issuer is refused ("issuer … does not route to realm
…"). Realms need kit with kit#939's realm rule (develop aca155cd or later).

**Issuers.** A home account reads and updates the tenant's own sign-in
issuer, the one its token comes from, and no other. It never creates or
deletes an issuer: another issuer is a new front door for signing people in,
so that stays the tenant owner's, from central. kit refuses it to a home
account whatever the tenant's catalog says, with `PermissionDenied`, and so
it does deleting the administration audience or Guard's. Declare
`authwise_issuer` in a configuration run with a central service account.

**What it does not, by design** (HA-D5). The tenant record and everything
the owner holds: `authwise_tenant_config` (branding, labels, settings),
`authwise_tenant_home_administration`, `authwise_service_account` and
`authwise_service_account_secret`, and the estate. kit refuses them with
`Unauthenticated` or `PermissionDenied`, and the provider adds why. Keep those in a configuration run with a central service
account the tenant owner created. Guard's own API (`authwise_guard_*` other
than the grants) is a separate audience and is out of a home account's
reach for now.

## Guard

The Guard resources talk to guard-control rather than kit. Set
`guard_endpoint` to its gRPC address; the provider sends it the same bearer
it sends `endpoint`, so the credential needs Guard's permissions too
(`guardcontrol.*`, which the guard-catalog example creates). Without
`guard_endpoint`, a Guard resource fails at plan. There is no Guard
tenant: Guard is keyed by the kit tenant (`t-…`), which the Guard resources
and data sources take as a required `tenant_id`, without inheriting the
provider's. `authwise_guard_tenant_settings` holds Guard's settings for a
tenant. A configuration or state that still holds a Guard tenant's id
(`gt-…`) fails at plan with the upgrade steps; the provider does not
rewrite it.

Estate relay pools (`authwise_guard_relay_pool`, `authwise_guard_pool_relay`,
`authwise_guard_relay_pool_access`) sit at the estate's root, not under a
tenant. guard-control admits them for an operator's credential only. A
tenant's network picks among its allowed pools through `relay_pool`. A
relay's credential is `authwise_guard_relay_credential`. The
guard-relay-pools example shows both halves.

## Scope defaults

Every Authwise object lives under a parent: a tenant, an issuer, a realm or
an audience. The provider's `tenant_id`, `issuer_id`, `realm_id` and
`audience_id` are defaults that every resource inherits. A resource's own
attribute of the same name overrides the default, and changing it replaces
the resource.

## Resource names and import

`name` on every resource is its full resource name, for example
`tenants/t-01/realms/r-01/providers/p-01`. It is the Terraform ID and the
import ID:

```shell
terraform import authwise_realm.employees tenants/t-01/realms/r-01
```

Most objects get an ID assigned by the server. A few take an ID you choose,
and they carry a required `<type>_id` attribute for it:
`authwise_access_permission`, `authwise_access_role`, `authwise_domain` and
`authwise_scope`.

## Configs built by data sources

Providers, factors and clients carry a type-specific config, packed as a
`google.protobuf.Any`. Each config type has a data source that builds it
with typed, validated attributes and makes no API call. Reference its `any`
output:

```terraform
data "authwise_provider_google" "this" {
  client_id         = "123456789012-abc.apps.googleusercontent.com"
  client_secret_ref = { name = authwise_secret.google.name }
}

resource "authwise_provider" "google" {
  realm_id      = "r-01"
  provider_type = "google"
  config        = data.authwise_provider_google.this.any
}
```

Issuers, realms and audiences carry a config that is one fixed message, and
an endpoint its `auth`. Each is the message's protojson encoding, and each
has a data source that builds it the same way. Reference its `json` output:

| data source | builds |
| --- | --- |
| `authwise_issuer_config` | `authwise_issuer.config` |
| `authwise_realm_config` | `authwise_realm.config` |
| `authwise_audience_config` | `authwise_audience.config` |
| `authwise_endpoint_auth` | `authwise_endpoint.auth` |

```terraform
data "authwise_realm_config" "this" {
  default_locale = "en"
  recovery       = { self_service_reset = true, reset_ttl = "30m" }
}

resource "authwise_realm" "this" {
  display_name = "Customers"
  config       = data.authwise_realm_config.this.json
}
```

Their attributes are the message's fields in snake_case, checked at plan.
Durations are Go strings such as `30m`, and the output leaves zero values
out, as the server does when it returns them.

Writing the JSON with `jsonencode` still works. Its keys are lowerCamel,
and zero values must be left out rather than written.

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `audience` (String) OAuth2 audience passed to the token endpoint. Falls back to `AUTHWISE_AUDIENCE`.
- `audience_id` (String) Default audience identifier; per-resource `audience_id` attributes override it.
- `client_id` (String) OAuth2 client ID for the client_credentials grant. Falls back to `AUTHWISE_CLIENT_ID`.
- `client_secret` (String, Sensitive) OAuth2 client secret. Falls back to `AUTHWISE_CLIENT_SECRET`.
- `endpoint` (String) gRPC API endpoint, e.g. `api.example.authwise.io:443`. Falls back to `AUTHWISE_ENDPOINT`.
- `guard_endpoint` (String) guard-control's gRPC endpoint, e.g. `guard.example.authwise.io:443`, for the `authwise_guard_*` resources and data sources. Falls back to `AUTHWISE_GUARD_ENDPOINT`. It takes the same credentials as `endpoint`. Unset, a Guard resource fails at plan.
- `insecure` (Boolean) Use plaintext gRPC (development only).
- `issuer_id` (String) Default issuer identifier; per-resource `issuer_id` attributes override it.
- `realm_id` (String) Default realm identifier; per-resource `realm_id` attributes override it.
- `tenant_id` (String) Default tenant identifier used to compose AIP parents; per-resource `tenant_id` attributes override it.
- `token_url` (String) OAuth2 token endpoint for the client_credentials grant, e.g. `https://auth.example.authwise.io/oauth/token`. Falls back to `AUTHWISE_TOKEN_URL`. When unset (with `client_id`), the provider uses the local awctl credential store instead.
