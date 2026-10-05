---
page_title: "authwise_issuer Resource - Authwise"
subcategory: ""
description: |-
  An OAuth 2.0 / OpenID Connect issuer: the login a set of clients shares, with its domain, token lifetimes and which realm or realms people sign in to (config). Its appearance is whichever of its profiles is the default (authwise_appearance_profile.is_default).
---

# authwise_issuer (Resource)

An issuer is the login that a set of clients share: its domain and path,
its token lifetimes, and which realm or realms people sign in to. Routing
between realms lives in `config`, in one of three provider selectors:

| selector | when |
| --- | --- |
| `singleRealmProviderSelector` | every login goes to one realm |
| `multiRealmProviderSelector` | **identifier-first**: the login page asks for an identifier, and ordered rules route it to a realm |
| `externalProviderSelector` | an endpoint decides |

## Example Usage: identifier-first login

In this example, consumers sign in with a password. Anyone at
`partner-co.example`, or any subdomain of it, goes straight to the partner's
SAML connection. The whole configuration, realms and providers included,
applies as it is from
[`examples/identifier-first`](../../examples/identifier-first). The
acceptance suite runs it the same way.

```terraform
resource "authwise_issuer" "login" {
  domain_name = "login.example.com"

  config = jsonencode({
    multiRealmProviderSelector = {
      realmNames = [
        authwise_realm.consumers.name,
        authwise_realm.partner.name,
      ]

      identifier = {
        label = "Work or personal email" # kind left out: EMAIL
      }

      rules = [
        {
          name    = "partner-co"
          domains = ["partner-co.example", "*.partner-co.example"]
          target = {
            realmName    = authwise_realm.partner.name
            providerName = authwise_provider.partner_saml.name
          }
        },
      ]

      defaultTarget = {
        realmName = authwise_realm.consumers.name
      }
    }
  })
}
```

## The routing selector

`multiRealmProviderSelector` holds four fields:

- **`realmNames`**: the realms this issuer serves, as realm names. Every
  target must name one of them. Invitations and SCIM admission read this
  list too.
- **`identifier`**: the identifier screen. It has two fields:
  - `label`: when empty, the label follows `kind` ("E-mail address" or
    "Username").
  - `kind`: `EMAIL` (the default; leave it out), `USERNAME` or `ANY`.
- **`rules`**: an ordered list, where the first match wins. Each rule has:
  - `name`: unique within the selector. Each login's audit row records it
    as `routing_rule`.
  - `domains`: exact (`partner.example`) or subdomain (`*.partner.example`,
    which does not match `partner.example` itself). They must be
    lower-case A-labels. The identifier's domain is the part after its last
    `@`.
  - `condition`: a CEL expression over `identifier`, `local_part` and
    `domain` that returns a bool, such as
    `local_part.endsWith(".ext")`. When a rule has both domains and a
    condition, both must hold.
  - `target`: `realmName`, plus an optional `providerName` in that realm.
    Without `providerName`, the realm's own selection applies: username and
    password when present, else its only provider, else a choice.
- **`defaultTarget`**: where an identifier that matches no rule goes.
  **Required.**

kit refuses a selector it could not honour, and the error names the field,
for example `config.multi_realm_provider_selector.rules[0].domains: …`. It
refuses:

- a selector with no `defaultTarget`, such as one with only `realmNames`;
- a target outside `realmNames`, a realm of another tenant, or a provider
  outside its target realm;
- a rule with no name, a duplicate name, or neither domains nor a
  condition;
- a malformed domain, or a condition that does not compile to a bool;
- a domain rule after an earlier rule without a condition has already
  taken that domain;
- any domain rule on a `USERNAME` identifier, since a username has no
  domain.

## Writing `config`

`config` is the protojson encoding of `IssuerConfig`, so:

- **Keys are lowerCamel**, such as `multiRealmProviderSelector` and
  `defaultTarget`. The value kit returns is compared with the value you
  wrote.
- **Leave zero values out** rather than writing them. kit stores what it is
  sent, but it omits zero values when it returns them, so an explicit
  `kind = "KIND_UNSPECIFIED"`, `label = ""` or `domains = []` comes back
  absent. Terraform then reports an inconsistent result after apply. Omit
  `kind` for `EMAIL`, or write `"EMAIL"`, which is not the zero value and
  round-trips.
- **The selector is written whole.** Any change to it replaces the whole
  member. kit refuses a partial update below
  `config.multiRealmProviderSelector`, and the provider never sends one.
- **References order the plan.** Naming `authwise_realm.x.name` and
  `authwise_provider.y.name` inside `jsonencode` creates the realms and
  providers first, and destroys them last.

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `config` (String) `config` as the protojson encoding of IssuerConfig.
- `domain_name` (String)
- `labels` (Map of String)
- `path` (String)
- `tenant_id` (String) Parent identifier `tenant_id`; overrides the provider default. Changing it replaces the resource.

### Read-Only

- `issuer_id` (String) Server-assigned resource id — the last segment of `name`, and what other resources' `*_id` attributes take.
- `name` (String) Full resource name; serves as the Terraform ID.

## Import

```shell
# Import by full resource name.
terraform import authwise_issuer.example tenants/t-01/issuers/i-01
```
