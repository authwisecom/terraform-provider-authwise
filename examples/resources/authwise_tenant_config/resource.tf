resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.png"
  mime_type    = "image/png"
}

resource "authwise_asset_content" "logo" {
  asset_id = authwise_asset.logo.asset_id
  source   = "${path.module}/logo.png"
}

# The logo is named through its content, so a destroy clears the brand
# before kit is asked to remove the content and the asset.
data "authwise_tenant_config" "this" {
  branding = {
    logo_asset_id = authwise_asset_content.logo.asset_id
  }
}

resource "authwise_tenant_config" "this" {
  config = data.authwise_tenant_config.this.json
}
