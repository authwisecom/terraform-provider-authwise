# The scope the example inherits. Substitute your install's tenant AWID.
#
# Endpoints are tenant-scoped, and so is everything an endpoint names: the
# secret, the issuer and the certificate must all be of the same tenant.
#
# Credentials come from the client_credentials lane (token_url / client_id /
# client_secret, or the AUTHWISE_* environment variables). Setting a secret
# reference needs identity.secrets.use on that credential.
provider "authwise" {
  endpoint = "api.example.authwise.com:443"

  tenant_id = "t-01"
}

terraform {
  required_version = ">= 1.11" # payload_wo is a write-only argument

  required_providers {
    authwise = {
      source = "registry.terraform.io/authwisecom/authwise"
    }
  }
}
