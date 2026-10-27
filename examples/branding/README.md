# Branding the login pages

The hosted login pages take their look from an **appearance profile**,
which is a **theme** plus overrides.

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
- **The default profile** is the issuer's appearance. It overrides the
  subheading.
- **The partners profile** uses the same theme with a different colour
  and heading. The partner portal client points at it through
  `appearance_profile_id`.

## Which profile is the default

The issuer's first profile becomes the default, whatever its `is_default`
says. Set `is_default = true` on the profile that should be the default,
and leave it out on the others. To move the default, set it on the new
profile and remove it from the old one in the same apply.

## Overrides

A profile's `stylesheet_attributes` and `content` override the theme's
key by key. A key the profile leaves out takes the theme's value.
