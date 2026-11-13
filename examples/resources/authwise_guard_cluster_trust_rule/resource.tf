# Argo CD's application controller pods join as ephemeral nodes of the
# cluster's group. StatefulSet pods that should keep their node across
# restarts get a rule without ephemeral.
resource "authwise_guard_cluster_trust_rule" "argocd" {
  tenant_id        = "t-01"
  network_id       = authwise_guard_network.office.guard_network_id
  cluster_trust_id = authwise_guard_cluster_trust.gfk.guard_cluster_trust_id
  namespace        = "argocd"
  service_account  = "argocd-application-controller"
  node_name_prefix = "argocd"
  ephemeral        = true
  node_group       = authwise_guard_node_group.gfk_pods.name
  max_nodes        = 10
}

# Every service account of the AWX job namespace.
resource "authwise_guard_cluster_trust_rule" "awx_jobs" {
  tenant_id        = "t-01"
  network_id       = authwise_guard_network.office.guard_network_id
  cluster_trust_id = authwise_guard_cluster_trust.gfk.guard_cluster_trust_id
  namespace        = "awx-jobs"
  service_account  = "*"
  node_name_prefix = "awx"
  ephemeral        = true
  node_group       = authwise_guard_node_group.gfk_pods.name
}
