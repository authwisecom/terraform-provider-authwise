# terraform-provider-authwise

The Authwise Terraform provider: a [tfinfra](https://github.com/activatedio/tfinfra)-
generated surface over the published
[apis](https://git.authwise.com/authwise/apis) — identity
resources, config-builder data sources, and provider-level scope defaults.

```hcl
terraform {
  required_providers {
    authwise = {
      source  = "authwisecom/authwise"
      version = "= <version>"
    }
  }
}

provider "authwise" {
  endpoint  = "api.example.authwise.io:443"   # or AUTHWISE_ENDPOINT
  tenant_id = "t-1"                           # default scope for resources

  # Client credentials (CI / automation); falls back to AUTHWISE_* env.
  # Omit all three to use the local `awctl auth login` credential store instead.
  token_url     = "https://auth.example.authwise.io/oauth/token"
  client_id     = "c-terraform"
  client_secret = var.authwise_client_secret
}

resource "authwise_realm" "employees" {
  display_name = "Employees"
}
```

## Ids and references

Every resource has two identifiers. `name` is the full resource name
(`tenants/t-01/issuers/i-01/audiences/a-01`), the Terraform ID and the
import ID. `<type>_id` is its last segment, the resource's own id:
`audience_id` on `authwise_audience`, `realm_id` on `authwise_realm`,
`client_id` on `authwise_client`. Data sources carry both.

The rule for references:

- **A `*_id` attribute takes another resource's `<type>_id`**, never its
  `name`. That covers the parent attributes (`tenant_id`, `issuer_id`,
  `realm_id`, `audience_id`, `client_id`) and the references kit resolves
  by id (a client's `audience_id` and `appearance_profile_id`, an
  appearance profile's `theme_id`, a binding's `condition_id`, the SAML
  configs' `*_certificate_id`).
- **A `*_ref = { name = … }` block and the association resources**
  (`authwise_access_role_access_permissions`,
  `authwise_scope_access_permissions`) **take `name`.**

```hcl
resource "authwise_client" "web" {
  display_name = "web"
  audience_id  = authwise_audience.api.audience_id   # not .name
}
```

Every id starts with its type's prefix (`a-` for an audience, `r-` for a
realm), and a reference is checked against it in plan: `audience_id =
authwise_audience.api.name` fails there, naming the attribute to use.
kit refuses the same mistake at apply (kit#620), and refuses an id that
exists under another issuer or tenant. An existing
`element(split("/", x.name), N)` still works, since it yields the id.
The SAML certificate fields are the one exception to the check: kit also
accepts a certificate's `name` there, so a configuration that passes one
keeps working, but `certificate_id` is the form to write.

Most resources get a server-assigned id, so `<type>_id` is computed. A few
are keyed by a name the caller chooses instead: the access catalog's
vocabulary (`authwise_access_permission`, `authwise_access_role`) and
`authwise_domain` / `authwise_scope`. On those `<type>_id` is required, and
changing it replaces the resource:

```hcl
resource "authwise_access_permission" "tenants_get" {
  access_permission_id = "guardcontrol.tenants.get"   # the id IS the name
  service              = "guardcontrol"

  # name (computed) = tenants/t-01/issuers/i-01/audiences/a-01
  #                     /access-permissions/guardcontrol.tenants.get
}
```

## The access surface

`authwise_access_permission`, `authwise_access_role`,
`authwise_access_role_access_permissions`, `authwise_access_condition` and
`authwise_access_binding` manage an audience's authorization catalog.
`examples/guard-catalog` is a complete worked example — guard-control's
prerequisite catalog, applied by the acceptance suite on every run.

Two things worth knowing:

- **Requires kit >= 1.10.0.** The role's permission set is refreshed
  through `ListAccessPermissionsByAccessRole`, which was not audience-scoped
  before that release (kit#312): against an older install a refresh reports
  other audiences' edges, inventing drift or hiding it.
- **`authwise_access_binding` validates `role_name` before writing.** The
  API accepts a binding naming a role that does not exist in its audience,
  stores it, and grants nothing — with a clean plan forever after (kit#296).
  The provider resolves the role in the binding's own audience first and
  fails the apply instead.

## Guard

The `authwise_guard_*` resources manage Guard, the mesh VPN, over
guard-control's API rather than kit's (#32): `authwise_guard_tenant`,
`authwise_guard_network`, `authwise_guard_relay`, `authwise_guard_resource`
with `authwise_guard_resource_nodes`, and `authwise_guard_invite`, each with
its singular and plural data sources. Set `guard_endpoint` (or
`AUTHWISE_GUARD_ENDPOINT`) to guard-control's gRPC address; the same bearer
goes to both services, and without it a Guard resource fails at plan.

`examples/guard` is a network from the tenant down to an invite, applied by
the acceptance suite together with `examples/guard-catalog`. Nodes and
Guard's users are not terraform's: they join by enrolment. That matters on
destroy, since guard-control refuses to delete a network that still has
nodes or to unregister a tenant with networks or users.

## Associations

Edges are managed as **authoritative set resources**
(`authwise_scope_access_permissions`,
`authwise_access_role_access_permissions`): the resource
owns the entity's full association set, so members associated out of band
are removed on the next apply. Members are full resource names; import by
the entity's full name.

```hcl
resource "authwise_access_role_access_permissions" "admin" {
  access_role        = authwise_access_role.admin.name
  access_permissions = [
    authwise_access_permission.read.name,
    authwise_access_permission.write.name,
  ]
}
```

(The awctl `add-*`/`remove-*` verbs are the imperative view of the same
API; the provider intentionally exposes only the declarative whole-set
form.)

## Secrets and references

A credential lives in an `authwise_secret`, and everything that needs it
names it: the social providers' `client_secret_ref`, the Duo factor's
`client_secret_ref`, and an endpoint's `auth`. The material goes in through
`payload_wo`, a **write-only** argument (Terraform 1.11 or later) — it is
never in the plan or state, and no API call returns it. To rotate, change
the material and bump `payload_wo_version`.

```hcl
resource "authwise_secret" "duo" {
  display_name       = "Duo client secret"
  payload_wo         = var.duo_client_secret
  payload_wo_version = 1
}

data "authwise_factor_duo" "duo" {
  client_id         = "DIXXXXXXXXXXXXXXXXXX"
  api_host          = "api-12345678.duosecurity.com"
  client_secret_ref = { name = authwise_secret.duo.name }
}

resource "authwise_factor" "duo" {
  realm_id     = "r-01"
  display_name = "Duo"
  factor_type  = "duo"
  config       = data.authwise_factor_duo.duo.any
}
```

Setting a reference needs `identity.secrets.use` on the provider's
credential. kit refuses to delete a secret while anything references it,
so reference it by expression, as above: Terraform then destroys or
repoints the referrer first.

A client's own secret works the other way around. You don't supply it:
kit mints it and returns it exactly once. So `authwise_client_secret` keeps
its `secret` in state, as a sensitive attribute, which makes the state
itself sensitive. Rotate it by changing a value in `keepers`:

```hcl
resource "authwise_client_secret" "ci" {
  client_id = authwise_client.ci.client_id
  keepers   = { rotation = "2026-09" }

  lifecycle {
    create_before_destroy = true
  }
}
```

## Authentication policy

`authwise_realm_authentication_policy` manages a realm's rules, floor,
enrollment, session, throttle, risk and acr levels
(`RealmConfig.authentication`) apart from `authwise_realm`, which leaves
that part of the realm's config alone and refuses an `authentication` key
in its own `config`. kit checks the policy on write, so a bad CEL
condition fails the apply with kit's reason. Destroying the policy clears
it and nothing else.

What kit accepts but doubts — a rule requiring a factor type no active
factor offers — comes back as a warning on the apply that caused it, and
`authwise_realm_authentication_context_schema` exposes the realm's current
warnings for a `check` block. `examples/authn` puts it all together.

## Installing in-progress builds

The provider is not on a public Terraform registry yet (that's tracked in
the release backlog). Until then, CI publishes every develop push as a dev
version (`X.Y.Z-<sha8>`) and every annotated tag as a release to the
project's GitLab package registry, and `scripts/install.sh` installs one
into Terraform's implied local mirror (`~/.terraform.d/plugins`):

```sh
GITLAB_TOKEN=<token with read_api> ./scripts/install.sh            # newest build
GITLAB_TOKEN=<token with read_api> ./scripts/install.sh 0.0.1-ab12cd34
```

Dev versions are semver prereleases: pin them exactly in
`required_providers` (`version = "= 0.0.1-ab12cd34"`).

## Authentication

Two lanes, the same bearer credentials as awctl:

- **Client credentials** — set `token_url`/`client_id`/`client_secret`
  (or `AUTHWISE_TOKEN_URL`/`AUTHWISE_CLIENT_ID`/`AUTHWISE_CLIENT_SECRET`,
  plus optional `audience`). Partial configuration is an error naming the
  gaps.
- **awctl store** — with no client credentials configured, the provider
  reads the token stored by `awctl auth login`
  (`~/.awctl.d/credentials/credentials.json`) — convenient for local
  development.

## Scope defaults

`tenant_id`, `issuer_id`, `realm_id`, and `audience_id` on the provider
block supply AIP parent segments so per-resource attributes can be
omitted; per-resource values always override.

## Tenants are not managed here

The provider works inside an existing tenant and never creates one.
(`authwise_guard_tenant` does not either: it registers an existing kit
tenant with Guard.) There
is no `authwise_tenant` resource. Tenants are provisioned out of band,
through the operator-facing tenancy admin API, which is not part of the
published contract. A configuration names its tenant with the provider's
`tenant_id`, or a resource's own. This was decided on 2026-08-15 (#11). A
read-only tenant data source can be added if a concrete need appears.

## Development

Most of the provider surface generates from `gen/main.go` — the declarative
spec table over the published pb types, mirroring awctl's table for
CLI/Terraform parity. The shapes tfinfra does not generate are hand-written
in `internal/provider`: `authwise_secret`, `authwise_realm_authentication_policy`,
and the wrappers around the generated `authwise_access_binding` and
`authwise_realm`.

```sh
make generate   # regenerate the provider surface from the spec table
make test       # unit tests
make testacc    # acceptance: real terraform CLI vs an in-memory AIP server
make build      # stamped binary in bin/
```

Never edit generated files by hand. Releases follow the Authwise
go-version standard: `make bump PUSH=true` pushes an annotated tag and the
tag pipeline publishes stamped multi-platform binaries.
