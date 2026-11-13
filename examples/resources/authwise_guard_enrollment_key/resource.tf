# The key a cluster's connector replicas join with, renewed before it
# expires: the replacement is a new key with a new secret.
resource "time_rotating" "connector_key" {
  rotation_days = 25
}

resource "authwise_guard_enrollment_key" "connector" {
  tenant_id        = "t-01"
  network_id       = authwise_guard_network.office.guard_network_id
  description      = "gfk connector (${time_rotating.connector_key.id})"
  expiry_days      = 30
  max_uses         = 4
  node_name_prefix = "gfk-connector"
  node_group       = authwise_guard_node_group.gfk_connector.name
}

# The Secret the Guard operator's GuardConnection reads.
resource "kubernetes_secret_v1" "guard_enrollment_key" {
  metadata {
    name      = "guard-enrollment-key"
    namespace = "guard-system"
  }
  data = {
    key = authwise_guard_enrollment_key.connector.secret
  }
}
