# Second-step authentication for a workforce realm, as Terraform:
#
#   - three factors the realm offers (TOTP, passkeys, Duo);
#   - Duo's client secret, held in an authwise_secret and named by reference;
#   - the realm's authentication policy: passkeys for admins, any second
#     factor for everyone else;
#   - a check that surfaces kit's warnings about the policy on every plan.

variable "duo_client_secret" {
  type      = string
  sensitive = true
  default   = "replace-me" # supply the real one from a secret store
}

# Rotating the Duo secret: change duo_client_secret and bump this.
variable "duo_client_secret_version" {
  type    = number
  default = 1
}

# Disabling is not deleting: people's enrolled passkeys survive a disable,
# not a destroy. Disable a factor to take it out of service temporarily.
variable "passkeys_status" {
  type    = string
  default = "active"
}

resource "authwise_realm" "workforce" {
  display_name = "Workforce"
  config       = jsonencode({ defaultLocale = "en" })
}

locals {
  realm_id = element(split("/", authwise_realm.workforce.name), 3)
}

# ---------------------------------------------------------------------------
# Factors
# ---------------------------------------------------------------------------

data "authwise_factor_totp" "totp" {
  # Every field has an interoperable default; SHA1 is deliberate (most
  # authenticator apps ignore the algorithm parameter).
  digits = 6
}

resource "authwise_factor" "totp" {
  realm_id     = local.realm_id
  display_name = "Authenticator app"
  factor_type  = "totp"
  config       = data.authwise_factor_totp.totp.any
}

data "authwise_factor_webauthn" "passkeys" {
  # Set once, before anybody enrols: changing rp_id orphans every credential.
  rp_id             = "login.example.com"
  rp_display_name   = "Example"
  user_verification = "required"
}

resource "authwise_factor" "passkeys" {
  realm_id     = local.realm_id
  display_name = "Passkeys"
  factor_type  = "webauthn"
  config       = data.authwise_factor_webauthn.passkeys.any
  status       = var.passkeys_status
}

# The material goes in write-only: it is not in the plan, not in state, and
# no API call returns it.
resource "authwise_secret" "duo" {
  display_name       = "Duo client secret"
  payload_wo         = var.duo_client_secret
  payload_wo_version = var.duo_client_secret_version
}

data "authwise_factor_duo" "duo" {
  client_id = "DIXXXXXXXXXXXXXXXXXX"
  api_host  = "api-12345678.duosecurity.com"

  # By reference, and by expression: kit refuses to delete a referenced
  # secret, and the expression is what makes Terraform remove the factor
  # first.
  client_secret_ref = { name = authwise_secret.duo.name }
}

resource "authwise_factor" "duo" {
  realm_id     = local.realm_id
  display_name = "Duo"
  factor_type  = "duo"
  config       = data.authwise_factor_duo.duo.any
}

# ---------------------------------------------------------------------------
# Policy
# ---------------------------------------------------------------------------

resource "authwise_realm_authentication_policy" "workforce" {
  realm = authwise_realm.workforce.name

  rules = [
    {
      name      = "admins"
      condition = "user.groups.exists(g, g == 'admins')"
      require = {
        mode                  = "TYPES"
        required_factor_types = ["webauthn"]
        reauth_after          = "15m"
      }
    },
    {
      name = "everyone"
      require = {
        mode                   = "ANY_FACTOR"
        skip_if_device_trusted = true
      }
    },
  ]

  # Every rule's outcome is raised to the floor, so a misjudged rule can
  # only ask for more.
  floor = {
    mode = "ANY_FACTOR"
  }

  enrollment = {
    in_flow            = true
    grace              = "72h"
    self_service_types = ["totp", "webauthn"]
  }

  remember_device = {
    enabled     = true
    ttl         = "720h"
    max_devices = 5
  }

  depends_on = [authwise_factor.totp, authwise_factor.passkeys, authwise_factor.duo]
}

# kit judges the stored policy against the factors the realm offers. A
# warning is not a refusal — kit accepted the write — so a check reports it
# on every plan without blocking the apply.
data "authwise_realm_authentication_context_schema" "workforce" {
  realm      = authwise_realm.workforce.name
  depends_on = [authwise_realm_authentication_policy.workforce]
}

check "authentication_policy" {
  assert {
    condition     = length(data.authwise_realm_authentication_context_schema.workforce.warnings) == 0
    error_message = join("\n", data.authwise_realm_authentication_context_schema.workforce.warnings)
  }
}
