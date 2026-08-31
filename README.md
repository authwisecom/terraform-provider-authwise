# terraform-provider-authwise

The Authwise Terraform provider: a [tfinfra](https://github.com/activatedio/tfinfra)-
generated surface over the published
[api-client-go](https://git.authwise.com/authwise/api-client-go) — identity
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
  display_name       = "Employees"
  user_database_type = "internal"
}
```

## Caller-named resources

Most resources get a server-assigned AWID, and `name` — the full AIP
resource name — is computed. A few are keyed by a name the caller chooses
instead: the access catalog's vocabulary (`authwise_access_permission`,
`authwise_access_role`) and `authwise_domain` / `authwise_scope`. Those
carry a required `<type>_id` attribute holding that id, and changing it
replaces the resource:

```hcl
resource "authwise_access_permission" "tenants_get" {
  access_permission_id = "guardcontrol.tenants.get"   # the id IS the name
  service              = "guardcontrol"

  # name (computed) = tenants/t-01/issuers/i-01/audiences/a-01
  #                     /access-permissions/guardcontrol.tenants.get
}
```

`name` stays the Terraform ID and the import ID; importing fills the id
attribute from the name's last segment.

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

## Associations

Role/permission edges are managed as **authoritative set resources**
(`authwise_user_roles`, `authwise_client_roles`,
`authwise_role_permissions`, `authwise_scope_permissions`,
`authwise_access_role_access_permissions`): the resource
owns the entity's full association set, so members associated out of band
are removed on the next apply. Members are full resource names; import by
the entity's full name.

```hcl
resource "authwise_role_permissions" "admin" {
  role        = authwise_role.admin.name
  permissions = [
    authwise_permission.read.name,
    authwise_permission.write.name,
  ]
}
```

(The awctl `add-*`/`remove-*` verbs are the imperative view of the same
API; the provider intentionally exposes only the declarative whole-set
form.)

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

## Development

The whole provider surface generates from `gen/main.go` — the declarative
spec table over the published pb types, mirroring awctl's table for
CLI/Terraform parity.

```sh
make generate   # regenerate the provider surface from the spec table
make test       # unit tests
make testacc    # acceptance: real terraform CLI vs an in-memory AIP server
make build      # stamped binary in bin/
```

Never edit generated files by hand. Releases follow the Authwise
go-version standard: `make bump PUSH=true` pushes an annotated tag and the
tag pipeline publishes stamped multi-platform binaries.
