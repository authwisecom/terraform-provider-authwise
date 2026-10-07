# A WireGuard mesh in the tenant. Leave cidr out for the whole of
# 100.64.0.0/10; it cannot change in place, and guard-control refuses to
# delete a network that still has nodes.
resource "authwise_guard_network" "office" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  display_name = "Office"
  cidr         = "100.96.0.0/16"
}
