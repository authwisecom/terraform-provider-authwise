# A server registered ahead of time. With no public_key it is PENDING: run
# the agent on the server with the auth code to enrol it. Enrolment writes
# public_key and endpoint, which read back without a diff.
resource "authwise_guard_node" "gateway" {
  tenant_id    = authwise_guard_tenant.acme.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "office-gateway"
}

output "gateway_auth_code" {
  value     = authwise_guard_node.gateway.auth_code
  sensitive = true
}
