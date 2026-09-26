# A REST service kit calls with a static bearer token:
#
#   - the token, held in an authwise_secret and named by reference — the
#     endpoint never holds the credential itself;
#   - the endpoint, with a per-call deadline of its own.

variable "risk_token" {
  type      = string
  sensitive = true
  default   = "replace-me" # supply the real one from a secret store
}

# Rotating the token: change risk_token and bump this.
variable "risk_token_version" {
  type    = number
  default = 1
}

resource "authwise_secret" "risk_token" {
  display_name       = "Risk service token"
  payload_wo         = var.risk_token
  payload_wo_version = var.risk_token_version
}

resource "authwise_endpoint" "risk" {
  display_name = "Risk service"

  # REST is the default transport: leave endpoint_type unset. The address is
  # the base URL a consumer appends its path to, and its scheme says whether
  # the connection is TLS — insecure is refused on a REST endpoint.
  address = "https://risk.example.com/v1"

  # Used when the caller sets no deadline of its own; 100ms to 60s, and the
  # install's default (5s) when unset.
  timeout = "2s"

  # auth is the protojson form of EndpointAuth, so its keys are lowerCamel.
  # Naming the secret here orders the endpoint after it.
  auth = jsonencode({
    bearer = {
      token = { name = authwise_secret.risk_token.name }
    }
  })
}
