resource "authwise_tenant_config" "this" {
  config = provider::authwise::tenant_config({
    access_token_expire_seconds  = 900
    refresh_token_expire_seconds = 1209600

    branding = {
      logo_asset_id = authwise_asset_content.logo.asset_id
    }
  })
}
