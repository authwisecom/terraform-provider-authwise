# Tenant-scoped: tenant_id comes from the provider default unless set here.
resource "authwise_realm" "employees" {
  display_name = "Employees"
  config       = jsonencode({ defaultLocale = "en" })

  labels = {
    team = "platform"
  }
}
