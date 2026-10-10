# guard-control's authorization catalog

The terraform path for the prerequisite guard-control needs on an Authwise
install: the `guardcontrol.*` permission vocabulary, the declared types
`guardcontrol.tenant` and `guardcontrol.network`, the `guardcontrol.admin`
role carrying the permissions and kit's link permissions, a binding to your
first admin, and the console's audience and OIDC client.

This is the same set of entries as guard-control's `docs/AUTHORIZATION.md`
Path A (static overlay, first install only) and Path B (awctl runbook, live
installs). Applying it is deployer-owned and stays deployer-owned: guard
neither applies nor reconciles these entries.

```sh
# Edit provider.tf (endpoint, scope AWIDs) and the locals at the top of
# main.tf (your admin's user id, the console URLs), then:
terraform init
terraform apply
```

The `console_client_id` output is what guard-admin is configured with
(`VITE_CLIENT_ID`, or the operator CR's `spec.admin.clientId`).

## Requirements

**kit >= 1.40.0** and **guard-control v0.12.0.** guard-control links each
Guard tenant under its parent Authwise tenant, and each network under its
Guard tenant, at central with the caller's own bearer. kit admits those
link writes at the link's parent from v1.40.0 (ESTATE_TENANCY.md E21), and
writes declared types to a database install from the same release
(kit#738). Against an older kit, every Guard tenant create is refused at the
link. The role's three `access.resourceLinks.*` permissions are kit's own:
the example reads them from the catalog and fails the plan if they are
missing.

kit's `authwise-guard` preset (kit#727) seeds these entries itself. Apply
this example on any other install.

## Upgrading guard-control

The permission list belongs to the guard-control version that generated it.
Regenerate it from the version you are deploying and **apply before rolling
out**:

```sh
guard-control authz-permissions          # guard-control's own: 41 at v0.12.0
guard-control authz-permissions --role   # what the role carries: those and kit's 3
```

Creating a permission nothing yet requires is harmless. Deploying a binary
that requires a permission nobody created is not: that method returns
permission-denied for everyone, with a healthy install and nothing in any log
explaining it.

Permissions guard-control has retired stay in the catalog and are inert;
removing them is optional tidying. A permission cannot be deleted while a role
still references it, so drop it from the `guardcontrol_permissions` list (which
removes the association) and delete the resource in the same apply.

## Things that fail silently

Terraform closes two of the four traps that page lists, and cannot close the
other two:

| | |
|---|---|
| Binding naming a role that does not exist in the audience | **Caught.** `authwise_access_binding` resolves `role_name` in its own audience before writing (kit#296). |
| Role with no permissions associated | **Caught.** The association resource owns the full set and reconciles it on every apply. |
| Console audience display name that is not guard-control's base URL, exactly | Yours to get right: it becomes the token's `aud` claim, which guard-control validates. |
| Redirect URI without its trailing slash | Yours to get right: guard-admin redirects to `${window.location.origin}/` and kit matches non-wildcard URIs exactly. |
