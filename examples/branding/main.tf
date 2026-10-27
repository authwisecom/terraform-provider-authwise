# Brand the hosted login pages: a logo, a theme built from the center
# layout's starter, the issuer's default appearance, and a second
# appearance one client signs in with.

# ---------------------------------------------------------------------------
# The logo
# ---------------------------------------------------------------------------

# The hosted pages serve an asset under /assets, so this one is at
# /assets/logo.svg.
resource "authwise_asset" "logo" {
  display_name = "Logo"
  path         = "/logo.svg"
  mime_type    = "image/svg+xml"
}

resource "authwise_asset_content" "logo" {
  asset_id = authwise_asset.logo.asset_id
  source   = "${path.module}/logo.svg"
}

# ---------------------------------------------------------------------------
# The theme
# ---------------------------------------------------------------------------

# center.json is the center layout as the issuer lists it:
#
#   curl -s https://login.example.com/api/v1/ui/layouts \
#     | jq '.layouts[] | select(.key == "center")' > center.json
#
# It carries the layout's two schemas and a starter theme: a stylesheet
# template, its attributes, and the page text.
locals {
  center = jsondecode(file("${path.module}/center.json"))
}

resource "authwise_theme" "brand" {
  display_name = "Example brand"

  # Fixed at creation: kit refuses a change, so another layout is another
  # theme.
  layout     = "center"
  stylesheet = local.center.starter.stylesheet

  # Colours are six hex digits without "#"; the stylesheet adds it.
  stylesheet_attributes = jsonencode(merge(local.center.starter.stylesheetAttributes, {
    primary_main   = "0B6E4F"
    link_color     = "0B6E4F"
    control_radius = "999px"
  }))

  content = jsonencode(merge(local.center.starter.content, {
    heading   = "Welcome"
    main_logo = "/assets${authwise_asset.logo.path}"
  }))

  stylesheet_attributes_schema = jsonencode(local.center.stylesheetAttributesSchema)
  content_schema               = jsonencode(local.center.contentSchema)

  # Point at the logo only once it is uploaded.
  depends_on = [authwise_asset_content.logo]
}

# ---------------------------------------------------------------------------
# Appearance profiles
# ---------------------------------------------------------------------------

# The issuer's appearance. A profile's stylesheet_attributes and content
# override the theme's, key by key.
resource "authwise_appearance_profile" "default" {
  display_name = "Example"
  theme_id     = authwise_theme.brand.theme_id
  is_default   = true

  content = jsonencode({
    subheading = "Sign in to Example"
  })
}

# Same theme, another face: the partner portal signs in with this one.
resource "authwise_appearance_profile" "partners" {
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

resource "authwise_audience" "portal" {
  display_name = "https://partners.example.com"
}

data "authwise_interactive_client_config" "portal" {
  allowed_redirect_uris = ["https://partners.example.com/callback"]
}

resource "authwise_client" "portal" {
  display_name          = "Partner portal"
  audience_id           = authwise_audience.portal.audience_id
  grant_types           = ["authorization_code", "refresh_token"]
  config                = data.authwise_interactive_client_config.portal.any
  appearance_profile_id = authwise_appearance_profile.partners.appearance_profile_id
}
