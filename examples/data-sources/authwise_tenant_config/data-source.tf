# The tenant is the outermost layer of the token lifetimes: an issuer,
# audience or client that sets its own overrides it.
data "authwise_tenant_config" "this" {
  access_token_expire_seconds  = 900
  refresh_token_expire_seconds = 1209600

  branding = {
    logo_asset_id      = authwise_asset_content.logo.asset_id
    logo_dark_asset_id = authwise_asset_content.logo_dark.asset_id
  }

  cors = {
    allowed_origins = ["https://admin.example.com"]
  }
}

resource "authwise_tenant_config" "this" {
  config = data.authwise_tenant_config.this.json
}
