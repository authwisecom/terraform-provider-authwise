# A Guard network, from the tenant down to the first invite.

locals {
  # The kit tenant to register with Guard: Guard's tenants are kit's.
  tenant_id = "t-01"
}

# 1. The tenant. Nothing else in Guard exists until it is registered.
resource "authwise_guard_tenant" "this" {
  guard_tenant_id = local.tenant_id
  display_name    = "Acme"
}

# 2. The network: a WireGuard mesh. Its cidr is the address space nodes get
#    addresses from, inside 100.64.0.0/10; changing it replaces the network.
resource "authwise_guard_network" "office" {
  tenant_id    = authwise_guard_tenant.this.guard_tenant_id
  display_name = "Office"
  cidr         = "100.96.0.0/16"
}

# 3. A relay, for nodes that cannot reach each other directly.
resource "authwise_guard_relay" "us_west" {
  tenant_id    = authwise_guard_tenant.this.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "us-west"
  url          = "wss://relay.example.com:8443/v1/transport"
  region       = "us-west1"
  priority     = 10
}

# 4. What people reach through the network: a subnet behind it and an
#    application by name.
resource "authwise_guard_resource" "office_lan" {
  tenant_id    = authwise_guard_tenant.this.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "Office LAN"
  kind         = "subnet"
  address      = "10.20.0.0/16"
}

resource "authwise_guard_resource" "wiki" {
  tenant_id    = authwise_guard_tenant.this.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "Wiki"
  kind         = "application"
  address      = "wiki.internal.example.com"
}

# 5. An invite for one person: the node they join with is granted both.
resource "authwise_guard_invite" "dana" {
  tenant_id    = authwise_guard_tenant.this.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "for Dana"
  grants = [
    authwise_guard_resource.office_lan.name,
    authwise_guard_resource.wiki.name,
  ]
}

output "invite_url" {
  description = "The join link carrying the invite's code. Shown once by guard-control; kept in state."
  value       = authwise_guard_invite.dana.url
  sensitive   = true
}
