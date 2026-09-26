# The scope the example inherits. Substitute your install's tenant AWID.
#
# Realms are tenant-scoped; the providers and the factor hang off the realms
# created in main.tf.
#
# Credentials come from the client_credentials lane (token_url / client_id /
# client_secret, or the AUTHWISE_* environment variables).
provider "authwise" {
  endpoint = "api.example.authwise.com:443"

  tenant_id = "t-01"
}

terraform {
  required_version = ">= 1.5"

  required_providers {
    authwise = {
      source = "registry.terraform.io/authwisecom/authwise"
    }
  }
}
