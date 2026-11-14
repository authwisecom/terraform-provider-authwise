# The issuer's default variant. A variant's stylesheet_attributes and
# content override its theme's, key by key.
resource "authwise_theme_variant" "default" {
  display_name = "Example"
  theme_id     = authwise_theme.brand.theme_id
  is_default   = true

  content = jsonencode({
    subheading = "Sign in to Example"
  })
}

# A second variant on the same theme, for one application.
resource "authwise_theme_variant" "partners" {
  display_name = "Example for partners"
  theme_id     = authwise_theme.brand.theme_id

  stylesheet_attributes = jsonencode({
    primary_main = "5B4B8A"
    link_color   = "5B4B8A"
  })

  content = jsonencode({
    heading    = "Partner portal"
    subheading = "Sign in with your partner account"
  })
}

resource "authwise_application" "portal" {
  display_name     = "Partner portal"
  audience_id      = authwise_audience.portal.audience_id
  grant_types      = ["authorization_code", "refresh_token"]
  config           = data.authwise_interactive_client_config.portal.any
  theme_variant_id = authwise_theme_variant.partners.theme_variant_id
}
