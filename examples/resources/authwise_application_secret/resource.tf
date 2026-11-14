# Application-scoped: a secret for a machine application. kit creates it and returns it
# once; Terraform keeps it in state and hands it on from there.
resource "authwise_application" "ci" {
  display_name = "CI"
  grant_types  = ["client_credentials"]
}

resource "authwise_application_secret" "ci" {
  application_id = authwise_application.ci.application_id
  expires_at     = "2027-01-01T00:00:00Z"

  # kit has no rotate verb: change a keeper to create a successor. With
  # create_before_destroy the new secret exists before the old one goes.
  keepers = {
    rotation = "2026-09"
  }

  lifecycle {
    create_before_destroy = true
  }
}

output "ci_client_secret" # the OAuth client_secret {
  value     = authwise_application_secret.ci.secret
  sensitive = true
}
