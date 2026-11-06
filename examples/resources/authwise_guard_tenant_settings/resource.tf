# Guard's settings for a kit tenant. Unset attributes are left as
# guard-control holds them; destroying this writes nothing.
resource "authwise_guard_tenant_settings" "acme" {
  tenant_id = "t-01"

  labels = {
    owner = "it"
  }

  config = {
    # Whose people are the tenant's; empty means every realm its issuer routes to.
    users = { realms = ["r-01"] }
    # Revoke the devices of people who no longer hold Guard.
    deprovisioning = { enabled = true }
  }
}
