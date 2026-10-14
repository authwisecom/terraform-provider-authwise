# Exactly one of bearer, basic, header and kit_token.
data "authwise_endpoint_auth" "this" {
  bearer = {
    token = { name = authwise_secret.risk_token.name }
  }
}

resource "authwise_endpoint" "this" {
  display_name = "Risk service"
  address      = "https://risk.example.com/v1"
  auth         = data.authwise_endpoint_auth.this.json
}
