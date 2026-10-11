# A subnet behind the network: reachable once a node serves it
# (authwise_guard_resource_nodes) and a node is granted it.
resource "authwise_guard_resource" "office_lan" {
  tenant_id    = "gt-01" # the Guard tenant, from awtenant
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "Office LAN"
  kind         = "subnet"
  address      = "10.20.0.0/16"
}
