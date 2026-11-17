# An issuer whose discovery document guard-control reads.
resource "authwise_guard_trusted_issuer" "build" {
  tenant_id    = "t-01"
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "build"
  issuer       = "https://id.build.example.com"
  discover     = true
}

# An issuer guard-control cannot reach: its keys are uploaded. When the
# issuer rotates its keys, refresh the file and apply.
resource "authwise_guard_trusted_issuer" "ops" {
  tenant_id    = "t-01"
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "ops"
  issuer       = "https://id.ops.internal"
  jwks         = file("${path.module}/ops-jwks.json")
}

# An issuer guard-control cannot reach, whose keys a node of the network
# reports: the node reads them where it runs, so they stay current with no
# upload. The issuer starts with no keys; the node's first report supplies
# them.
resource "authwise_guard_trusted_issuer" "lab" {
  tenant_id       = "t-01"
  network_id      = authwise_guard_network.office.guard_network_id
  display_name    = "lab"
  issuer          = "https://id.lab.internal"
  key_source_node = authwise_guard_node.lab_reporter.name
}

# The audience a joining machine's token must carry.
output "build_audience" {
  value = authwise_guard_trusted_issuer.build.audience
}
