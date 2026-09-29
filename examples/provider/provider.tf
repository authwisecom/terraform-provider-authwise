provider "authwise" {
  endpoint = "api.example.authwise.com:443" # or AUTHWISE_ENDPOINT

  # Scope defaults: a resource inherits these unless it sets its own
  # tenant_id / issuer_id / realm_id / audience_id.
  tenant_id = "t-01"
  issuer_id = "i-01"

  # Client credentials, for CI and automation; each falls back to its
  # AUTHWISE_* environment variable. Omit all of them to use the local
  # `awctl auth login` credential store instead.
  token_url     = "https://auth.example.authwise.com/oauth/token"
  client_id     = "c-terraform"
  client_secret = var.authwise_client_secret
}

variable "authwise_client_secret" {
  type      = string
  sensitive = true
}
