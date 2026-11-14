# Branding the login pages

The hosted login pages take their look from a **theme**: the base of a
brand. A **theme variant** is a theme changed for one issuer: it names its
theme and holds only what differs. What applies, in order: the
application's variant, then the audience's, then the issuer's default
variant, then the tenant's default theme (`authwise_tenant_branding`).

- **The logo** is an asset. The pages serve every asset under `/assets`,
  so `path = "/logo.svg"` is at `/assets/logo.svg`. That URL is what
  `content.main_logo` names.
- **The theme** starts from the center layout's starter.
  [`center.json`](center.json) is that layout as an issuer lists it:

  ```sh
  curl -s https://login.example.com/api/v1/ui/layouts \
    | jq '.layouts[] | select(.key == "center")' > center.json
  ```

  It carries a stylesheet template, the attributes the template reads,
  the page text, and a JSON Schema for each. The theme takes the starter
  whole and overrides a few keys with `merge`. Colours are six hex digits
  without `#`, because the template adds it. `layout` is fixed at
  creation: to change layout, create another theme.
- **The default variant** is the issuer's. It overrides the subheading.
- **The partners variant** uses the same theme with a different colour
  and heading. The partner portal application points at it through
  `theme_variant_id`.

## Which variant is the default

The issuer's first variant becomes the default, whatever its `is_default`
says. Set `is_default = true` on the variant that should be the default,
and leave it out on the others. To move the default, set it on the new
variant and remove it from the old one in the same apply. An issuer with
no variant uses the tenant's default theme.

## Overrides

A variant's `stylesheet_attributes` and `content` override the theme's
key by key. A key the variant leaves out takes the theme's value.
