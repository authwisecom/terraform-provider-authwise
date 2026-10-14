data "authwise_guard_deprovisioning" "this" {
  tenant_id = "gt-01"
}

output "last_sweep" {
  value = {
    result  = data.authwise_guard_deprovisioning.this.last_result
    message = data.authwise_guard_deprovisioning.this.last_message
    revoked = data.authwise_guard_deprovisioning.this.last_revoked
  }
}
