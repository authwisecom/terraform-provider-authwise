# Passwordless sign-in, two ways:
#
#   - a customer realm offering a password or an emailed link and code;
#   - a workforce realm signing in with passkeys alone.

# ---------------------------------------------------------------------------
# Password or magic link
# ---------------------------------------------------------------------------

resource "authwise_realm" "customers" {
  display_name = "Customers"
}

locals {
  customers_realm_id = element(split("/", authwise_realm.customers.name), 3)
}

resource "authwise_provider" "password" {
  realm_id      = local.customers_realm_id
  display_name  = "Password"
  provider_type = "usernamePassword"
}

data "authwise_provider_magic_link" "email" {
  # Every field has a default: a 6-digit code, a 10-minute ttl (NIST's
  # ceiling for an out-of-band secret), a link and a code in one mail, and
  # identifiers matched on email.
  ttl  = "10m"
  mode = "LINK_AND_CODE" # CODE_ONLY when people read mail on another device
}

resource "authwise_provider" "magic_link" {
  realm_id      = local.customers_realm_id
  display_name  = "Email me a link"
  provider_type = "magicLink"
  config        = data.authwise_provider_magic_link.email.any
}

# ---------------------------------------------------------------------------
# Passkeys
# ---------------------------------------------------------------------------

resource "authwise_realm" "workforce" {
  display_name = "Workforce"
}

locals {
  workforce_realm_id = element(split("/", authwise_realm.workforce.name), 3)
}

# The passkey provider has no relying party of its own: it signs people in
# with the credentials of the realm's webauthn factor. Without an enabled
# one, kit refuses the first sign-in ("realm offers no webauthn factor").
data "authwise_factor_webauthn" "passkeys" {
  # Set once, before anybody enrols: changing rp_id orphans every credential.
  rp_id           = "login.example.com"
  rp_display_name = "Example"
  resident_key    = "required" # a passkey is a discoverable credential
}

resource "authwise_factor" "passkeys" {
  realm_id     = local.workforce_realm_id
  display_name = "Passkeys"
  factor_type  = "webauthn"
  config       = data.authwise_factor_webauthn.passkeys.any
}

data "authwise_provider_passkey" "passkey" {}

resource "authwise_provider" "passkey" {
  realm_id      = local.workforce_realm_id
  display_name  = "Passkey"
  provider_type = "passkey"
  config        = data.authwise_provider_passkey.passkey.any

  # Ordered after the factor so the provider never exists without one.
  depends_on = [authwise_factor.passkeys]
}
