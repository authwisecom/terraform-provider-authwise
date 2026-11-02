# An API that is not an OpenID audience, with roles in its tokens.
resource "authwise_audience" "this" {
  display_name = "https://api.example.com"
  config = provider::authwise::audience_config({
    access_token_expire_seconds = 600

    scopes = {
      openid        = false
      authorization = { include_roles = true }
    }
  })
}
