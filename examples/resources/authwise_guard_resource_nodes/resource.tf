# The nodes that serve a resource, as an authoritative set: a node
# associated out of band is removed on the next apply.
resource "authwise_guard_resource_nodes" "office_lan" {
  guard_resource = authwise_guard_resource.office_lan.name
  nodes          = [authwise_guard_node.gateway.name]
}
