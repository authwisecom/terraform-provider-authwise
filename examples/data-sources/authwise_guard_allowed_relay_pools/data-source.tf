# The pools a tenant's networks may use, with the tenant's credential.
data "authwise_guard_allowed_relay_pools" "acme" {
  tenant_id = "t-01"
}

output "pool_slugs" {
  value = data.authwise_guard_allowed_relay_pools.acme.relay_pools[*].slug
}
