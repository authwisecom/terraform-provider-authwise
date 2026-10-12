# The account's secret. kit mints it and returns it once; Terraform keeps it
# in state and hands it on from there.
resource "authwise_service_account_secret" "ci" {
  service_account = authwise_service_account.ci.name
  expires_at      = "2027-01-01T00:00:00Z"

  # Change a keeper to rotate: the new secret is minted and the old one keeps
  # working for the grace (24 hours unless set), so the pipeline has that long
  # to pick up the new one. Destroying this resource does not revoke the
  # secret: it lapses at its expiry, at a rotation, or with the account.
  grace = "48h"
  keepers = {
    rotation = "2026-10"
  }
}

# What the pipeline needs, as AUTHWISE_TOKEN_URL, AUTHWISE_CLIENT_ID,
# AUTHWISE_CLIENT_SECRET and AUTHWISE_AUDIENCE.
output "ci_credential" {
  value = {
    token_url     = authwise_service_account_secret.ci.token_url
    client_id     = authwise_service_account.ci.client_id
    client_secret = authwise_service_account_secret.ci.secret
    audience      = authwise_service_account_secret.ci.audience
  }
  sensitive = true
}
