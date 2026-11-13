# Argo CD's pods reach the git server and every ops host; the ops hosts
# reach the cluster's pods. Grants are authoritative sets of resource, node
# and node group names.
resource "authwise_guard_node_group_grants" "gfk_pods" {
  node_group = authwise_guard_node_group.gfk_pods.name
  grants = [
    authwise_guard_resource.git.name,
    authwise_guard_node_group.ops.name,
  ]
}

resource "authwise_guard_node_group_grants" "ops" {
  node_group = authwise_guard_node_group.ops.name
  grants     = [authwise_guard_node_group.gfk_pods.name]
}
