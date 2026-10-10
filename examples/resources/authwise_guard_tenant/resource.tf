# A Guard tenant under a kit tenant. guard-control assigns its id (gt-…),
# which the rest of Guard's resources take as tenant_id. The users link is
# where the tenant's people sign in and are decided; it and the parent are
# set at create only, so changing either replaces the tenant. Destroying it
# deletes the tenant, which guard-control refuses while it still has
# networks or users.
resource "authwise_guard_tenant" "acme" {
  parent_tenant_id = "t-01"
  display_name     = "Acme"
  users = {
    issuer          = "https://id.example.authwise.com/t-01/i-01"
    audience        = "a-02"
    access_endpoint = "api.example.authwise.com:443"
  }
}
