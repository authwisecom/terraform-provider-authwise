---
page_title: "authwise_issuer Resource - Authwise"
subcategory: "Identity"
description: |-
  An OAuth 2.0 / OpenID Connect issuer: the login a set of clients shares, with its domain, token lifetimes and which realm or realms people sign in to (config). Its appearance is whichever of its profiles is the default (authwise_appearance_profile.is_default).
---

# authwise_issuer (Resource)

An issuer is the login that a set of clients share: its domain and path,
its token lifetimes, and which realm or realms people sign in to. Routing
between realms lives in `config`, in one of three provider selectors:

| selector | when |
| --- | --- |
| `single_realm_provider_selector` | every login goes to one realm |
| `multi_realm_provider_selector` | **identifier-first**: the login page asks for an identifier, and ordered rules route it to a realm |
| `external_provider_selector` | an endpoint decides |

Build `config` with the `authwise_issuer_config` data source and pass its
`json`.

## Example Usage: identifier-first login

In this example, consumers sign in with a password. Anyone at
`partner-co.example`, or any subdomain of it, goes straight to the partner's
SAML connection. The whole configuration, realms and providers included,
applies as it is from
[`examples/identifier-first`](../../examples/identifier-first). The
acceptance suite runs it the same way.

```terraform
data "authwise_issuer_config" "login" {
  multi_realm_provider_selector = {
    realm_names = [
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
          realm_name    = authwise_realm.partner.name
          provider_name = authwise_provider.partner_saml.name
        }
      },
    ]

    default_target = {
      realm_name = authwise_realm.consumers.name
    }
  }
}

resource "authwise_domain" "login" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "login" {
  domain_id = authwise_domain.login.domain_id
  config    = data.authwise_issuer_config.login.json
}
```

## The routing selector

`multi_realm_provider_selector` holds four fields:

- **`realm_names`**: the realms this issuer serves, as realm names. Every
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
  - `target`: `realm_name`, plus an optional `provider_name` in that realm.
    Without `provider_name`, the realm's own selection applies: username and
    password when present, else its only provider, else a choice.
- **`default_target`**: where an identifier that matches no rule goes.
  **Required.**

kit refuses a selector it could not honour, and the error names the field,
for example `config.multi_realm_provider_selector.rules[0].domains: …`. It
refuses:

- a selector with no `default_target`, such as one with only `realm_names`;
- a target outside `realm_names`, a realm of another tenant, or a provider
  outside its target realm;
- a rule with no name, a duplicate name, or neither domains nor a
  condition;
- a malformed domain, or a condition that does not compile to a bool;
- a domain rule after an earlier rule without a condition has already
  taken that domain;
- any domain rule on a `USERNAME` identifier, since a username has no
  domain.

## Writing `config`

`config` is the protojson encoding of `IssuerConfig`.

- **`authwise_issuer_config` builds it.** Its attributes are the message's
  fields in snake_case, checked at plan: a misspelt field, an unknown
  `kind` or a second selector fails there, not at apply. Its `json` leaves
  zero values out, as kit does when it returns them.
- **The selector is written whole.** Any change to it replaces the whole
  member. kit refuses a partial update below
  `config.multiRealmProviderSelector`, and the provider never sends one.
- **References order the plan.** Naming `authwise_realm.x.name` and
  `authwise_provider.y.name` in the data source creates the realms and
  providers first, and destroys them last.
- **`jsonencode` works too.** Its keys are then lowerCamel, such as
  `multiRealmProviderSelector` and `defaultTarget`, because the value kit
  returns is compared with the value you wrote. Leave zero values out
  rather than writing them: kit omits them when it returns them, so an
  explicit `kind = "KIND_UNSPECIFIED"`, `label = ""` or `domains = []`
  comes back absent and Terraform reports an inconsistent result after
  apply.

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `domain_id` (String)

### Optional

- `config` (String) `config` as the protojson encoding of IssuerConfig.
- `labels` (Map of String)
- `path` (String)
- `tenant_id` (String) Parent identifier `tenant_id`; overrides the provider default, and reads back from the name. Changing a configured one replaces the resource.

### Read-Only

- `domain_name` (String)
- `issuer_id` (String) Server-assigned resource id — the last segment of `name`, and what other resources' `*_id` attributes take.
- `name` (String) Full resource name; serves as the Terraform ID.

## Import

```shell
# Import by full resource name.
terraform import authwise_issuer.example tenants/t-01/issuers/i-01
```
