# Which pools a tenant may use beyond the estate default, and its own
# default pool. Operators only; authoritative.
resource "authwise_guard_relay_pool_access" "acme" {
  tenant_id     = "t-01" # the kit tenant Guard is on
  allowed_pools = [authwise_guard_relay_pool.eu_west.name]
  default_pool  = authwise_guard_relay_pool.eu_west.name
}
