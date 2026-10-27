resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.svg"
  mime_type    = "image/svg+xml"
}

# The file the hosted pages serve at /assets plus the asset's path, here
# /assets/logo.svg, the URL a theme's main_logo names. Editing logo.svg
# plans an in-place update that uploads it again.
resource "authwise_asset_content" "logo" {
  asset_id = authwise_asset.logo.asset_id
  source   = "${path.module}/logo.svg"
}
