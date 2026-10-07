# A relay the network's nodes fall back to when no direct path exists.
resource "authwise_guard_relay" "us_west" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "us-west"
  url          = "wss://relay.example.com:8443/v1/transport"
  region       = "us-west1"
  priority     = 10
}
