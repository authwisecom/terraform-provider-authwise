# Guard on a kit tenant: its settings, a network, down to a gateway server.

locals {
  # The kit tenant (t-…) Guard is on. There is no Guard tenant: Guard's
  # networks and settings are keyed by the kit tenant, and kit reports
  # Guard on it before guard-control takes a write.
  guard_tenant_id = "t-01"
}

# 0. Guard's settings for the tenant: the sweep that revokes the devices of
#    people who lost Guard. The slug that names the tenant in mesh DNS is
#    kit's, set on the tenant in kit (awtenant tenants update --slug,
#    kit 1.83.0 or later).
#    Destroying this leaves the settings in place.
resource "authwise_guard_tenant_settings" "this" {
  tenant_id = local.guard_tenant_id

  config = {
    deprovisioning = { enabled = true }
  }
}

# 1. The network: a WireGuard mesh. Its cidr is the address space nodes get
#    addresses from, inside 100.64.0.0/10; changing it replaces the network.
resource "authwise_guard_network" "office" {
  tenant_id    = local.guard_tenant_id
  display_name = "Office"
  cidr         = "100.96.0.0/16"
}

# 2. A relay, for nodes that cannot reach each other directly.
resource "authwise_guard_relay" "us_west" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "us-west"
  url          = "wss://relay.example.com:8443/v1/transport"
  region       = "us-west1"
  priority     = 10
}

# 3. What people reach through the network: a subnet behind it and an
#    application by name.
resource "authwise_guard_resource" "office_lan" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "Office LAN"
  kind         = "subnet"
  address      = "10.20.0.0/16"
}

resource "authwise_guard_resource" "wiki" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "Wiki"
  kind         = "application"
  address      = "wiki.internal.example.com"
}

# 4. The gateway: a server in the office that routes to the LAN. It is
#    registered here and enrols itself with the auth code, which writes its
#    key and endpoint; those read back without a diff.
resource "authwise_guard_node" "gateway" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "office-gateway"
}

resource "authwise_guard_resource_nodes" "office_lan" {
  guard_resource = authwise_guard_resource.office_lan.name
  nodes          = [authwise_guard_node.gateway.name]
}

output "gateway_auth_code" {
  description = "Enrols the gateway: run the agent on it with this code. Shown once by guard-control; kept in state."
  value       = authwise_guard_node.gateway.auth_code
  sensitive   = true
}
