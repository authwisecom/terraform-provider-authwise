resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.svg"
  mime_type    = "image/svg+xml"
}

# The file the hosted pages serve at the asset's path. Editing logo.svg
# plans an in-place update that uploads it again.
resource "authwise_asset_content" "logo" {
  asset_id = authwise_asset.logo.asset_id
  source   = "${path.module}/logo.svg"
}
