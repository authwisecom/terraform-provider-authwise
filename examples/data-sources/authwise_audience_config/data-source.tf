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

# An API that wants the caller's name and email in the bearer itself. They
# are copied only when the grant includes the scope.
data "authwise_audience_config" "orders" {
  access_token_claim_scopes = ["profile", "email"]
}

resource "authwise_audience" "orders" {
  display_name = "https://orders.example.com"
  config       = data.authwise_audience_config.orders.json
}
