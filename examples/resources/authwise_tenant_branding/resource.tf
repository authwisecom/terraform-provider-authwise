# The tenant's brand: its name in mail (and the From name), its look, and
# its logo. Leave branding out of authwise_tenant_config: set it here only.
resource "authwise_tenant_branding" "this" {
  display_name  = "Northwind Supply"
  theme_id      = authwise_theme.northwind.theme_id
  logo_asset_id = authwise_asset_content.logo.asset_id
}
