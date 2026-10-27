# The scope the example inherits. Substitute your install's tenant and
# issuer AWIDs.
#
# The theme and the asset are tenant-scoped; the appearance profiles, the
# audience and the client belong to the issuer.
#
# Credentials come from the client_credentials lane (token_url / client_id /
# client_secret, or the AUTHWISE_* environment variables).
provider "authwise" {
  endpoint = "api.example.authwise.com:443"

  tenant_id = "t-01"
  issuer_id = "i-01"
}

terraform {
  required_version = ">= 1.5"

  required_providers {
    authwise = {
      source = "registry.terraform.io/authwisecom/authwise"
    }
  }
}
