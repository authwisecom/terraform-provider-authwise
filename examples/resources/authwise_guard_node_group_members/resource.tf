# A group of servers registered by hand. Don't manage the members of a
# group that an enrollment key or a cluster trust rule fills: this resource
# would remove the nodes they add.
resource "authwise_guard_node_group" "ops" {
  tenant_id    = "t-01"
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "ops hosts"
}

resource "authwise_guard_node_group_members" "ops" {
  node_group = authwise_guard_node_group.ops.name
  nodes = [
    authwise_guard_node.bastion.name,
    authwise_guard_node.awx.name,
  ]
}
