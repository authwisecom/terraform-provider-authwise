# guard-control's authorization catalog

The terraform path for the prerequisite guard-control needs on an Authwise
install: the `guardcontrol.*` permission vocabulary, the `guardcontrol.admin`
role carrying the permissions an operator administers with, a root binding
to your first admin, and the console's audience and OIDC client.

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

**guard-control v0.27.0.** The list is generated from guard-control
v0.27.0 (c9c1034): 59 permissions, 52 of them on `guardcontrol.admin`. The
other seven, `guardcontrol.networks.enrol` and `guardcontrol.self.*`, are a
person's own. The catalog declares them, but the admin role does not carry
them.

**Applying over a copy from before guard-control v0.27.0.** The apply
removes three things guard-control no longer uses:

- the declared types `guardcontrol.tenant` and `guardcontrol.network`, which
  are destroyed (unused since guard-control#80, the tenant collapse);
- kit's three `access.resourceLinks.*` permissions, which are taken off
  `guardcontrol.admin` (also unused since guard-control#80);
- the four `guardcontrol.invites.*` permissions, which are destroyed
  (retired in guard-control#83).

guard-control's docs/AUTHORIZATION.md calls all of these inert. kit's link
permissions themselves stay: they are kit's, so the example never deletes
them.

kit's `authwise-guard` preset (kit#727) seeds these entries itself. Apply
this example on any other install.

## Upgrading guard-control

The permission list belongs to the guard-control version that generated it.
Regenerate it from the version you are deploying and **apply before rolling
out**:

```sh
guard-control authz-permissions --role   # admin_permissions: 52 at v0.27.0
guard-control authz-permissions          # admin_permissions + user_permissions: 59
```

Creating a permission nothing yet requires is harmless. Deploying a binary
that requires a permission nobody created is not: that method returns
permission-denied for everyone, with a healthy install and nothing in any log
explaining it.

Permissions guard-control has retired stay in the catalog and are inert;
removing them is optional tidying. A permission cannot be deleted while a role
still references it, so drop it from `admin_permissions` or `user_permissions` (which
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
