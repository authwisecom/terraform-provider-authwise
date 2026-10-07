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
  audience_id  = authwise_audience.api.audience_id
  grant_types  = ["authorization_code", "refresh_token"]
  config       = data.authwise_interactive_client_config.console.any
}
