# The scope every resource in main.tf inherits: kit's admin tenant, issuer
# and access-bearing audience. Substitute your install's AWIDs.
#
# Credentials come from the client_credentials lane (token_url / client_id /
# client_secret, or the AUTHWISE_* environment variables); for interactive
# use, `awctl auth login` writes the local store the provider falls back to.
provider "authwise" {
  endpoint = "api.example.authwise.com:443"

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
