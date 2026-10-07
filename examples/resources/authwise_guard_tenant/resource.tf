# Register a kit tenant with Guard. guard_tenant_id is the kit tenant's AWID.
# Destroying it unregisters the tenant, which guard-control refuses while
# the tenant still has networks or users.
resource "authwise_guard_tenant" "acme" {
  guard_tenant_id = "t-01"
  display_name    = "Acme"
}
