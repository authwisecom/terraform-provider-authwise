# Audience-scoped and caller-named: the id is the role's own name, and the
# permissions it grants are an authoritative set.
resource "authwise_access_permission" "tenants_get" {
  access_permission_id = "guardcontrol.tenants.get"
}

resource "authwise_access_role" "admin" {
  access_role_id = "guardcontrol.admin"
  display_name   = "Administrator"
  description    = "Full console administration."
}

resource "authwise_access_role_access_permissions" "admin" {
  access_role = authwise_access_role.admin.name
  access_permissions = [
    authwise_access_permission.tenants_get.name,
  ]
}
