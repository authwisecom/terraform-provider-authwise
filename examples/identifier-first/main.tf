# Identifier-first login, as Terraform: one login page that asks for an
# email address and routes it.
#
#   - consumers sign in to the Consumers realm with a password — the default
#     for any address no rule matches;
#   - anybody at partner-co.example, or a subdomain of it, is sent straight
#     to the Partner realm's SAML connection, never seeing a password field.

resource "authwise_realm" "consumers" {
  display_name = "Consumers"
}

resource "authwise_provider" "password" {
  realm_id      = authwise_realm.consumers.realm_id
  display_name  = "Password"
  provider_type = "usernamePassword"
}

resource "authwise_realm" "partner" {
  display_name = "Partner Co"
}

resource "authwise_certificate" "partner_idp_signing" {
  display_name           = "Partner IdP signing"
  use                    = "CERTIFICATE_USE_SIGNING"
  import_certificate_pem = file("${path.module}/partner-idp-signing.crt")
}

data "authwise_provider_saml" "partner" {
  idp_entity_id              = "https://idp.partner-co.example/metadata"
  idp_sso_url                = "https://idp.partner-co.example/sso"
  idp_signing_certificate_id = authwise_certificate.partner_idp_signing.certificate_id
  want_assertions_signed     = true
  name_id_format             = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
}

resource "authwise_provider" "partner_saml" {
  realm_id      = authwise_realm.partner.realm_id
  display_name  = "Partner Co (SAML)"
  provider_type = "saml"
  config        = data.authwise_provider_saml.partner.any
}

# The issuer's config, typed: authwise_issuer_config checks the attribute
# names and enum values at plan and writes the protojson for config.
data "authwise_issuer_config" "login" {
  multi_realm_provider_selector = {
    # The realms this issuer serves. Every target below must be one of
    # them; invitations and SCIM admission read this list too.
    realm_names = [
      authwise_realm.consumers.name,
      authwise_realm.partner.name,
    ]

    # The identifier screen. kind is left out: that is EMAIL. Set
    # "USERNAME" or "ANY" to change it.
    identifier = {
      label = "Work or personal email"
    }

    # Ordered; the first match wins. The name is recorded on each login's
    # audit row as routing_rule.
    rules = [
      {
        name    = "partner-co"
        domains = ["partner-co.example", "*.partner-co.example"]
        target = {
          realm_name    = authwise_realm.partner.name
          provider_name = authwise_provider.partner_saml.name
        }
      },
    ]

    # Everyone else. Required. With no provider_name, the realm's own
    # selection applies: its password provider here.
    default_target = {
      realm_name = authwise_realm.consumers.name
    }
  }
}

resource "authwise_issuer" "login" {
  domain_name = "login.example.com"

  # The selector is written whole on every change.
  config = data.authwise_issuer_config.login.json
}
