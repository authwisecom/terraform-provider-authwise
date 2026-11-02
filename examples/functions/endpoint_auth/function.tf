# Exactly one of bearer, basic, header and kit_token.
resource "authwise_endpoint" "this" {
  display_name = "Risk service"
  address      = "https://risk.example.com/v1"
  auth = provider::authwise::endpoint_auth({
    bearer = {
      token = { name = authwise_secret.risk_token.name }
    }
  })
}
