# A GKE cluster, whose issuer guard-control reads.
resource "authwise_guard_cluster_trust" "gfk" {
  tenant_id    = "t-01"
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "gfk-nonprod-uc1"
  issuer       = "https://container.googleapis.com/v1/projects/acme-nonprod/locations/us-central1-a/clusters/gfk-nonprod-uc1"
  discover     = true
}

# An on-premises cluster whose issuer guard-control cannot reach: its keys
# are uploaded, from `kubectl get --raw /openid/v1/jwks > ops-jwks.json`.
# When the cluster rotates its keys, refresh the file and apply.
resource "authwise_guard_cluster_trust" "ops" {
  tenant_id    = "t-01"
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "ops-rke2"
  issuer       = "https://kubernetes.default.svc.cluster.local"
  jwks         = file("${path.module}/ops-jwks.json")
}

# The audience the Guard operator in the cluster sets on the token it
# projects.
output "gfk_audience" {
  value = authwise_guard_cluster_trust.gfk.audience
}
