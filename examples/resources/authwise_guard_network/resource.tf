# A WireGuard mesh in the tenant. Leave cidr out for the whole of
# 100.64.0.0/10; it cannot change in place, and guard-control refuses to
# delete a network that still has nodes.
resource "authwise_guard_network" "office" {
  tenant_id    = "t-01" # the kit tenant Guard is on
  display_name = "Office"
  cidr         = "100.96.0.0/16"

  # An estate relay pool the tenant may use, by name (a slug is refused);
  # unset, the tenant's default pool. pool_fallback serves it after the
  # network's own relays.
  relay_pool = "relay-pools/rp-01"
}
