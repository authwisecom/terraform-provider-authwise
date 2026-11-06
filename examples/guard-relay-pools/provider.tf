# guard_endpoint is guard-control; endpoint is kit. Both take the same
# credential (token_url / client_id / client_secret, or the AUTHWISE_*
# environment variables).
provider "authwise" {
  endpoint       = "api.example.authwise.com:443"
  guard_endpoint = "guard.example.authwise.com:443"

  tenant_id   = "t-01"
  issuer_id   = "i-01"
  audience_id = "a-01"
}

terraform {
  required_providers {
    authwise = {
      source = "registry.terraform.io/authwisecom/authwise"
    }
  }
}
