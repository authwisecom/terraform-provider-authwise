# The pods of one Kubernetes cluster that join the network. A cluster trust
# rule or an enrollment key that names the group adds each node it enrolls,
# so a pod that joins is served and reachable at once.
resource "authwise_guard_node_group" "gfk_pods" {
  tenant_id    = "t-01"
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "gfk pods"
  description  = "Pods of the gfk-nonprod-uc1 cluster"
}
