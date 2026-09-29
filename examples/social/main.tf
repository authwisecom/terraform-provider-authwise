# Social and enterprise sign-in, as Terraform. Every provider follows the
# same three steps:
#
#   1. the client secret (Apple: the .p8 key) goes into an authwise_secret,
#      write-only — never into the plan, the state, or the provider;
#   2. the config data source builds the typed config, naming that secret by
#      reference;
#   3. an authwise_provider on the realm takes the data source's any, with
#      display_name as the button label.
#
# provider_type must match the config's type: google, microsoft, github,
# facebook, apple, oidc, okta, auth0, linkedin or oauth.

variable "client_secrets" {
  description = "Client secrets by provider, from your secret store."
  type        = map(string)
  sensitive   = true
  default = {
    google    = "replace-me"
    microsoft = "replace-me"
    github    = "replace-me"
    okta      = "replace-me"
    discord   = "replace-me"
  }
}

variable "apple_p8" {
  description = "Apple's .p8 signing key, PEM."
  type        = string
  sensitive   = true
  default     = "replace-me"
}

resource "authwise_realm" "customers" {
  display_name = "Customers"
}

locals {
  realm_id = element(split("/", authwise_realm.customers.name), 3)
}

resource "authwise_secret" "client" {
  for_each = toset(["google", "microsoft", "github", "okta", "discord"])

  display_name = "${each.key} client secret"
  payload_wo   = var.client_secrets[each.key]
}

# ---------------------------------------------------------------------------
# Google
# ---------------------------------------------------------------------------

data "authwise_provider_google" "this" {
  client_id         = "123456789012-abc.apps.googleusercontent.com"
  client_secret_ref = { name = authwise_secret.client["google"].name }

  # An empty list is Google's default: openid email profile.
  scopes = ["openid", "email", "profile"]

  # Only these Workspace domains may sign in (the hd claim, verified).
  hosted_domains = ["example.com"]

  # Sent verbatim on the authorization request. kit refuses the parameters
  # it sets itself (client_id, redirect_uri, scope, state, ...).
  authorization_params = {
    prompt = "select_account"
  }
}

resource "authwise_provider" "google" {
  realm_id      = local.realm_id
  display_name  = "Sign in with Google"
  provider_type = "google"
  config        = data.authwise_provider_google.this.any
}

# ---------------------------------------------------------------------------
# Microsoft
# ---------------------------------------------------------------------------

data "authwise_provider_microsoft" "this" {
  client_id         = "00000000-0000-0000-0000-000000000000"
  client_secret_ref = { name = authwise_secret.client["microsoft"].name }

  # Directory (tenant) IDs allowed to sign in; empty allows any.
  allowed_tenants = ["11111111-1111-1111-1111-111111111111"]
}

resource "authwise_provider" "microsoft" {
  realm_id      = local.realm_id
  display_name  = "Sign in with Microsoft"
  provider_type = "microsoft"
  config        = data.authwise_provider_microsoft.this.any
}

# ---------------------------------------------------------------------------
# GitHub
# ---------------------------------------------------------------------------

data "authwise_provider_github" "this" {
  client_id         = "Iv1.0123456789abcdef"
  client_secret_ref = { name = authwise_secret.client["github"].name }
  scopes            = ["read:user", "user:email"]

  authorization_params = {
    allow_signup = "false"
  }
}

resource "authwise_provider" "github" {
  realm_id      = local.realm_id
  display_name  = "Sign in with GitHub"
  provider_type = "github"
  config        = data.authwise_provider_github.this.any
}

# ---------------------------------------------------------------------------
# Apple: the .p8 key signs a fresh client-secret JWT on every exchange, so
# the secret is the key itself.
# ---------------------------------------------------------------------------

resource "authwise_secret" "apple_p8" {
  display_name = "Sign in with Apple key"
  payload_wo   = var.apple_p8
}

data "authwise_provider_apple" "this" {
  client_id       = "com.example.web" # the Services ID
  team_id         = "TEAM123456"
  key_id          = "KEY1234567"
  private_key_ref = { name = authwise_secret.apple_p8.name }
}

resource "authwise_provider" "apple" {
  realm_id      = local.realm_id
  display_name  = "Sign in with Apple"
  provider_type = "apple"
  config        = data.authwise_provider_apple.this.any
}

# ---------------------------------------------------------------------------
# Any OpenID Connect provider (Okta here; Keycloak, Ping, another Authwise
# alike): discovery from the issuer, endpoints pinned only when they must be.
# ---------------------------------------------------------------------------

data "authwise_provider_oidc" "okta" {
  issuer            = "https://example.okta.com/oauth2/default"
  client_id         = "0oa0123456789abcdef"
  client_secret_ref = { name = authwise_secret.client["okta"].name }
  scopes            = ["openid", "email", "profile"]
}

resource "authwise_provider" "okta" {
  realm_id      = local.realm_id
  display_name  = "Sign in with Okta"
  provider_type = "oidc"
  config        = data.authwise_provider_oidc.okta.any
}

# ---------------------------------------------------------------------------
# Plain OAuth 2.0, no id_token (Discord here): explicit endpoints, and the
# identity read from the userinfo document through a claim map — both
# required, since there is nothing to default to.
# ---------------------------------------------------------------------------

data "authwise_provider_oauth" "discord" {
  client_id         = "012345678901234567"
  client_secret_ref = { name = authwise_secret.client["discord"].name }
  authorization_url = "https://discord.com/oauth2/authorize"
  token_url         = "https://discord.com/api/oauth2/token"
  userinfo_url      = "https://discord.com/api/users/@me"
  scopes            = ["identify", "email"]

  identifier_source = "userinfo.id"
  claim_map = {
    map = {
      email              = "userinfo.email"
      preferred_username = "userinfo.username"
    }
  }
}

resource "authwise_provider" "discord" {
  realm_id      = local.realm_id
  display_name  = "Sign in with Discord"
  provider_type = "oauth"
  config        = data.authwise_provider_oauth.discord.any
}
