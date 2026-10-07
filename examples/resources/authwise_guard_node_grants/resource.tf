# What a node may reach, as an authoritative set of resource and node names
# of its network. Grants an invite gave the node are removed unless listed.
resource "authwise_guard_node_grants" "build" {
  guard_node = authwise_guard_node.build.name
  grants = [
    authwise_guard_resource.office_lan.name,
    authwise_guard_node.gateway.name,
  ]
}
