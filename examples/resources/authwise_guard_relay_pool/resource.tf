# An estate relay pool, the default every tenant may use. Operators only.
resource "authwise_guard_relay_pool" "us_central" {
  slug         = "us-central"
  display_name = "US central"
  region       = "us-central1"
  select       = "nearest" # by each node's region; or "served-order"
  default      = true
}
