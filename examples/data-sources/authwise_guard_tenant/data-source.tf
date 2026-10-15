# Look a Guard tenant up by its slug, so a configuration never hard-codes a
# gt-… id. The slug is only an alias: pass guard_tenant_id on.
data "authwise_guard_tenant" "millcreek" {
  slug = "millcreek"
}

resource "authwise_guard_enrolment_grant" "engineering" {
  tenant_id    = data.authwise_guard_tenant.millcreek.guard_tenant_id
  subject_type = "group"
  subject_id   = "g-01"
}

# Or read it by its full resource name.
data "authwise_guard_tenant" "by_name" {
  name = "tenants/gt-01"
}
