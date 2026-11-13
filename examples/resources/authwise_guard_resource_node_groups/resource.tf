# Prometheus in the cluster, served by every replica of the cluster's
# connector. Translated: clients reach it at its published name, so another
# cluster may serve the same cluster address.
resource "authwise_guard_resource" "prometheus" {
  tenant_id      = "t-01"
  network_id     = authwise_guard_network.office.guard_network_id
  display_name   = "Prometheus (gfk)"
  kind           = "application"
  address        = "prometheus.monitoring.svc.cluster.local"
  translated     = true
  published_name = "prometheus.gfk.office.acme.guard.authwise.com"
}

resource "authwise_guard_resource_node_groups" "prometheus" {
  resource    = authwise_guard_resource.prometheus.name
  node_groups = [authwise_guard_node_group.gfk_connector.name]
}
