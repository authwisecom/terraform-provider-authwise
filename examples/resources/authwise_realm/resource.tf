# Tenant-scoped: tenant_id comes from the provider default unless set here.
resource "authwise_realm" "employees" {
  display_name = "Employees"
  config       = data.authwise_realm_config.employees.json

  labels = {
    team = "platform"
  }
}

data "authwise_realm_config" "employees" {
  default_locale = "en"

  recovery = {
    self_service_reset = true
    reset_ttl          = "30m"
  }
}
