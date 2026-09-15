# The scope the SAML entries inherit. Substitute your install's AWIDs.
#
# Certificates are tenant-scoped, providers are realm-scoped and clients are
# issuer-scoped, so main.tf sets realm_id per provider resource; tenant and
# issuer come from here.
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
