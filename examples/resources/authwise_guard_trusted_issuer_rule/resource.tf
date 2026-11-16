# The deploy runners join as ephemeral nodes of the runners' group: each
# holds a token the issuer signed with sub "deploy-runner".
resource "authwise_guard_trusted_issuer_rule" "deploy" {
  tenant_id         = "t-01"
  network_id        = authwise_guard_network.office.guard_network_id
  trusted_issuer_id = authwise_guard_trusted_issuer.build.guard_trusted_issuer_id
  subject           = "deploy-runner"
  node_name_prefix  = "deploy"
  ephemeral         = true
  node_group        = authwise_guard_node_group.runners.name
  max_nodes         = 10
}

# A second subject is a second rule: subjects are matched exactly.
resource "authwise_guard_trusted_issuer_rule" "backup" {
  tenant_id         = "t-01"
  network_id        = authwise_guard_network.office.guard_network_id
  trusted_issuer_id = authwise_guard_trusted_issuer.build.guard_trusted_issuer_id
  subject           = "backup-agent"
  node_name_prefix  = "backup"
}
