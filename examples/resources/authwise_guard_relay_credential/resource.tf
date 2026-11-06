# The pool relay's credential, issued once and kept in state.
resource "authwise_guard_relay_credential" "us_central_a" {
  relay = authwise_guard_pool_relay.us_central_a.name

  # Change to rotate: the resource is replaced and a new credential issued.
  rotation = "2026-11"
}

# Hand it to the relay's VM, which writes it to its token file.
output "us_central_a_relay_token" {
  value     = authwise_guard_relay_credential.us_central_a.credential
  sensitive = true
}
