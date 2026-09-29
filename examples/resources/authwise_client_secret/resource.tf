# Client-scoped: a secret for a machine client. kit mints it and returns it
# once; Terraform keeps it in state and hands it on from there.
resource "authwise_client" "ci" {
  display_name = "CI"
  grant_type   = "client_credentials"
}

resource "authwise_client_secret" "ci" {
  client_id  = authwise_client.ci.client_id
  expires_at = "2027-01-01T00:00:00Z"

  # kit has no rotate verb: change a keeper to mint a successor. With
  # create_before_destroy the new secret exists before the old one goes.
  keepers = {
    rotation = "2026-09"
  }

  lifecycle {
    create_before_destroy = true
  }
}

output "ci_client_secret" {
  value     = authwise_client_secret.ci.secret
  sensitive = true
}
