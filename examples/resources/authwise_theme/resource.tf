# A theme from the center layout's starter. center.json is the layout as
# the issuer lists it:
#
#   curl -s https://login.example.com/api/v1/ui/layouts \
#     | jq '.layouts[] | select(.key == "center")' > center.json
#
# examples/branding applies this with its logo and appearance profiles.
locals {
  center = jsondecode(file("${path.module}/center.json"))
}

resource "authwise_theme" "brand" {
  display_name = "Example brand"

  # Fixed at creation: kit refuses a change, so another layout is another
  # theme.
  layout     = "center"
  stylesheet = local.center.starter.stylesheet

  # The starter's values, with ours on top. Colours are six hex digits
  # without "#"; the stylesheet adds it.
  stylesheet_attributes = jsonencode(merge(local.center.starter.stylesheetAttributes, {
    primary_main   = "0B6E4F"
    link_color     = "0B6E4F"
    control_radius = "999px"
  }))

  # main_logo names an asset by its served URL: authwise_asset path
  # "/logo.svg" is served at "/assets/logo.svg".
  content = jsonencode(merge(local.center.starter.content, {
    heading   = "Welcome"
    main_logo = "/assets${authwise_asset.logo.path}"
  }))

  stylesheet_attributes_schema = jsonencode(local.center.stylesheetAttributesSchema)
  content_schema               = jsonencode(local.center.contentSchema)

  depends_on = [authwise_asset_content.logo]
}
