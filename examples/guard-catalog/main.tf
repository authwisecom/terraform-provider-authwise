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
# Requires kit >= 1.40.0. guard-control links each Guard tenant under its
# parent, and each network under its Guard tenant, at central with the
# caller's own bearer: kit admits those link writes at the link's parent
# from that release (ESTATE_TENANCY.md E21), and writes the declared types
# below to a database install (kit#738). Before it, every Guard tenant
# create is refused at the link.
#
# kit's authwise-guard preset (kit#727) seeds all of this itself: the
# permissions, the role and the declared types. Apply this on any other
# install.

locals {
  # Generated from guard-control v0.12.0's proto. Do not hand-maintain a copy —
  # regenerate from the version you deploy, and apply the new version's
  # entries BEFORE rolling it out:
  #
  #   guard-control authz-permissions
  #
  # A permission nothing yet requires is harmless; a binary that requires a
  # permission nobody created denies that method for everyone, with a healthy
  # install and nothing in any log explaining it.
  guardcontrol_permissions = [
    "guardcontrol.events.get",
    "guardcontrol.events.list",
    "guardcontrol.events.search",
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
    "guardcontrol.tenants.create",
    "guardcontrol.tenants.delete",
    "guardcontrol.tenants.get",
    "guardcontrol.tenants.list",
    "guardcontrol.tenants.update",
    "guardcontrol.users.create",
    "guardcontrol.users.delete",
    "guardcontrol.users.get",
    "guardcontrol.users.list",
    "guardcontrol.users.update",
  ]

  # kit's own permissions the role carries beside guard-control's: the link
  # writes guard-control makes with the caller's bearer, and the list an
  # unlink finds the link with. kit declares them, so they are read, not
  # created (`guard-control authz-permissions --role` prints all 44).
  kit_permissions = [
    "access.resourceLinks.create",
    "access.resourceLinks.delete",
    "access.resourceLinks.list",
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

# 2. The declared types a grant reaches Guard through. A Guard tenant is
#    linked under its parent Authwise tenant, and a network under its Guard
#    tenant, so a binding at the parent reaches both. Without them the link
#    guard-control writes is refused, and so is the create.
resource "authwise_access_resource_type" "guardcontrol_tenant" {
  access_resource_type_id = "guardcontrol.tenant"
  parent_type             = "tenant"
  expansion_mode          = "PUSH"
  description             = "A Guard tenant, administered through its parent Authwise tenant"
}

resource "authwise_access_resource_type" "guardcontrol_network" {
  access_resource_type_id = "guardcontrol.network"
  parent_type             = authwise_access_resource_type.guardcontrol_tenant.access_resource_type_id
  expansion_mode          = "PUSH"
  description             = "A Guard network, administered through its Guard tenant"
}

# 3. What you actually grant.
resource "authwise_access_role" "guardcontrol_admin" {
  access_role_id = "guardcontrol.admin"
  description    = "Full guard-control console administration."
}

# 4. The role's permissions, as an authoritative set: a permission associated
#    out of band is removed on the next apply, and a role that exists but
#    carries no permissions grants a bundle of nothing while looking correct.
#    kit's link permissions are read from the role's own catalog, so an
#    install without them fails the plan here.
data "authwise_access_permission" "kit" {
  for_each = toset(local.kit_permissions)
  name     = "${trimsuffix(authwise_access_role.guardcontrol_admin.name, "/access-roles/${authwise_access_role.guardcontrol_admin.access_role_id}")}/access-permissions/${each.value}"
}

resource "authwise_access_role_access_permissions" "guardcontrol_admin" {
  access_role = authwise_access_role.guardcontrol_admin.name
  access_permissions = concat(
    [for p in authwise_access_permission.guardcontrol : p.name],
    [for p in data.authwise_access_permission.kit : p.name],
  )
}

# 5. The binding. Without it the console loads and everything 403s.
#
#    resource_type and resource_id are left unset, which anchors the grant at
#    the root so it applies on every Guard tenant. Two narrower anchors:
#    resource_type = "tenant" with an Authwise tenant's AWID reaches every
#    Guard tenant under it; resource_type = "guardcontrol.tenant" with a
#    `gt-…` id reaches one Guard tenant.
resource "authwise_access_binding" "guardcontrol_admin" {
  subject_type = "user"
  subject_id   = local.admin_subject_id
  role_name    = authwise_access_role.guardcontrol_admin.access_role_id
}

# 6. The console's audience and OIDC client. Without these the console cannot
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
