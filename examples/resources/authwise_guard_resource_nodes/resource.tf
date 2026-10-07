# The nodes that serve a resource, as an authoritative set: a node
# associated out of band is removed on the next apply. Nodes join the
# network by enrolment, so they are named here rather than created.
resource "authwise_guard_resource_nodes" "office_lan" {
  guard_resource = authwise_guard_resource.office_lan.name
  nodes = [
    "${authwise_guard_network.office.name}/nodes/nd-01",
  ]
}
