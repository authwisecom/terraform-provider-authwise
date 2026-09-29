# Issuer-scoped: an OIDC client for a browser app, calling the API its
# audience names.
resource "authwise_audience" "api" {
  display_name = "https://api.example.com"
}

data "authwise_interactive_client_config" "console" {
  allowed_redirect_uris = ["https://console.example.com/callback"]
}

resource "authwise_client" "console" {
  display_name = "Console"
  audience_id  = element(split("/", authwise_audience.api.name), 5)
  grant_type   = "authorization_code"
  config       = data.authwise_interactive_client_config.console.any
}
