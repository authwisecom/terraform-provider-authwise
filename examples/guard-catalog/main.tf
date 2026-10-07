# guard-control's authorization catalog, as Terraform.
#
# An Authwise install has never heard of Guard: it does not know what
# `guardcontrol.networks.create` means until someone tells it, and until it
# does, guard-control runs correctly and every console request is denied.
# Creating these entries is the deployer's responsibility, the way IAM policy
# is on AWS. This is the terraform path; guard-control's
# docs/AUTHORIZATION.md carries the same entries as an awctl runbook and as a
# first-install static overlay.
#
# Requires kit >= 1.10.0: the role's permission set is refreshed through
# ListAccessPermissionsByAccessRole, which was not audience-scoped before
# that release (kit#312) and would report other audiences' edges as drift.

locals {
  # Generated from guard-control v0.8.1's proto. Do not hand-maintain a copy —
  # regenerate from the version you deploy, and apply the new version's
  # entries BEFORE rolling it out:
  #
  #   guard-control authz-permissions
  #
  # A permission nothing yet requires is harmless; a binary that requires a
  # permission nobody created denies that method for everyone, with a healthy
  # install and nothing in any log explaining it.
  guardcontrol_permissions = [
    "guardcontrol.invites.create",
    "guardcontrol.invites.delete",
    "guardcontrol.invites.get",
    "guardcontrol.invites.list",
    "guardcontrol.networks.create",
    "guardcontrol.networks.delete",
    "guardcontrol.networks.enrol",
    "guardcontrol.networks.get",
    "guardcontrol.networks.list",
    "guardcontrol.networks.update",
    "guardcontrol.nodes.associateGrants",
    "guardcontrol.nodes.create",
    "guardcontrol.nodes.delete",
    "guardcontrol.nodes.get",
    "guardcontrol.nodes.issueAuthCode",
    "guardcontrol.nodes.list",
    "guardcontrol.nodes.update",
    "guardcontrol.relays.create",
    "guardcontrol.relays.delete",
    "guardcontrol.relays.get",
    "guardcontrol.relays.list",
    "guardcontrol.relays.update",
    "guardcontrol.resources.associateNodes",
    "guardcontrol.resources.create",
    "guardcontrol.resources.delete",
    "guardcontrol.resources.get",
    "guardcontrol.resources.list",
    "guardcontrol.resources.update",
    "guardcontrol.tenants.get",
    "guardcontrol.tenants.register",
    "guardcontrol.tenants.unregister",
    "guardcontrol.tenants.update",
    "guardcontrol.users.create",
    "guardcontrol.users.delete",
    "guardcontrol.users.get",
    "guardcontrol.users.list",
    "guardcontrol.users.update",
  ]

  # Your first console admin: a user that already exists in the install.
  admin_subject_id = "u-01"

  # The console's public base URL. This becomes the audience display name and
  # therefore the token's `aud` claim, which guard-control validates — it must
  # match character for character, scheme and trailing slash included.
  console_api_url = "https://guard.example.com/api"

  # guard-admin redirects to `${window.location.origin}/` and kit matches
  # non-wildcard redirect URIs exactly, so the trailing slash is required.
  console_redirect_uri = "https://guard-admin.example.com/"

  # The audience AWID kit minted, which the client and any token request
  # reference.
  console_audience_id = authwise_audience.console.audience_id
}

# 1. The vocabulary. One permission per guard-control API method, named by the
#    deployer rather than the server — access_permission_id IS the name the
#    authorization engine matches on.
resource "authwise_access_permission" "guardcontrol" {
  for_each = toset(local.guardcontrol_permissions)

  access_permission_id = each.value
  service              = "guardcontrol"
}

# 2. What you actually grant.
resource "authwise_access_role" "guardcontrol_admin" {
  access_role_id = "guardcontrol.admin"
  description    = "Full guard-control console administration."
}

# 3. The role's permissions, as an authoritative set: a permission associated
#    out of band is removed on the next apply, and a role that exists but
#    carries no permissions grants a bundle of nothing while looking correct.
resource "authwise_access_role_access_permissions" "guardcontrol_admin" {
  access_role        = authwise_access_role.guardcontrol_admin.name
  access_permissions = [for p in authwise_access_permission.guardcontrol : p.name]
}

# 4. The binding. Without it the console loads and everything 403s.
#
#    resource_type and resource_id are left unset, which anchors the grant at
#    the root so it applies on every tenant. To scope it to one tenant, set
#    resource_type = "tenant" and resource_id = <tenant awid>.
resource "authwise_access_binding" "guardcontrol_admin" {
  subject_type = "user"
  subject_id   = local.admin_subject_id
  role_name    = authwise_access_role.guardcontrol_admin.access_role_id
}

# 5. The console's audience and OIDC client. Without these the console cannot
#    start a login at all. Unlike the entries above — which are the install
#    learning Guard's vocabulary, once per install — this pair carries one
#    deployment's URLs.
resource "authwise_audience" "console" {
  display_name = local.console_api_url
}

data "authwise_interactive_client_config" "console" {
  allowed_redirect_uris = [local.console_redirect_uri]
}

# guard-admin is a public SPA client: authorization_code + PKCE, no secret.
resource "authwise_client" "console" {
  display_name = "guard-admin"
  audience_id  = local.console_audience_id
  grant_types  = ["authorization_code", "refresh_token"]
  config       = data.authwise_interactive_client_config.console.any
}

output "console_client_id" {
  description = "Configure guard-admin with this (VITE_CLIENT_ID, or the operator CR's spec.admin.clientId)."
  value       = authwise_client.console.client_id
}

output "console_audience_id" {
  description = "The console audience AWID."
  value       = local.console_audience_id
}
