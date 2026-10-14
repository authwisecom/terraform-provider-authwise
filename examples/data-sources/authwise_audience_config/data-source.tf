# An API that is not an OpenID audience, with roles in its tokens.
data "authwise_audience_config" "this" {
  access_token_expire_seconds = 600

  scopes = {
    openid        = false
    authorization = { include_roles = true }
  }
}

resource "authwise_audience" "this" {
  display_name = "https://api.example.com"
  config       = data.authwise_audience_config.this.json
}
