# A relay of an estate pool. Changing pool moves it in place.
resource "authwise_guard_pool_relay" "us_central_a" {
  pool         = authwise_guard_relay_pool.us_central.name
  display_name = "us-central-a"
  url          = "wss://a.us-central.relay.example.com/v1/transport"
  region       = "us-central1"
}
