# Estate relay pools (guard-control#81): an operator's pools, a relay in
# one with its credential, a tenant allowed a second pool, and a network on
# it. In practice the operator's half and the tenant's half are separate
# roots with separate credentials; they share one here.

locals {
  tenant_id = "t-01" # the kit tenant Guard is on
}

# --- The operator's: the estate's pools and their relays ---

resource "authwise_guard_relay_pool" "us_central" {
  slug         = "us-central"
  display_name = "US central"
  region       = "us-central1"
  select       = "nearest"
  default      = true # the estate default: every tenant may use it
}

resource "authwise_guard_relay_pool" "eu_west" {
  slug         = "eu-west"
  display_name = "EU west"
  region       = "europe-west1"
}

resource "authwise_guard_pool_relay" "us_central_a" {
  pool         = authwise_guard_relay_pool.us_central.name
  display_name = "us-central-a"
  url          = "wss://a.us-central.relay.example.com/v1/transport"
  region       = "us-central1"
}

resource "authwise_guard_relay_credential" "us_central_a" {
  relay    = authwise_guard_pool_relay.us_central_a.name
  rotation = "1"
}

# The tenant may use eu-west too, and takes it as its default.
resource "authwise_guard_relay_pool_access" "acme" {
  tenant_id     = local.tenant_id
  allowed_pools = [authwise_guard_relay_pool.eu_west.name]
  default_pool  = authwise_guard_relay_pool.eu_west.name
}

# --- The tenant's: a network on a pool ---

resource "authwise_guard_network" "office" {
  tenant_id    = local.tenant_id
  display_name = "Office"
  relay_pool   = authwise_guard_relay_pool.eu_west.name # the name; a slug is refused

  # The pool must be allowed before the network names it.
  depends_on = [authwise_guard_relay_pool_access.acme]
}

output "us_central_a_relay_token" {
  value     = authwise_guard_relay_credential.us_central_a.credential
  sensitive = true
}
