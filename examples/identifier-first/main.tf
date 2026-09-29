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
  realm_id      = element(split("/", authwise_realm.consumers.name), 3)
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
  idp_signing_certificate_id = authwise_certificate.partner_idp_signing.name
  want_assertions_signed     = true
  name_id_format             = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
}

resource "authwise_provider" "partner_saml" {
  realm_id      = element(split("/", authwise_realm.partner.name), 3)
  display_name  = "Partner Co (SAML)"
  provider_type = "saml"
  config        = data.authwise_provider_saml.partner.any
}

resource "authwise_issuer" "login" {
  domain_name = "login.example.com"

  # config is IssuerConfig as protojson, so its keys are lowerCamel. The
  # selector is written whole on every change.
  config = jsonencode({
    multiRealmProviderSelector = {
      # The realms this issuer serves. Every target below must be one of
      # them; invitations and SCIM admission read this list too.
      realmNames = [
        authwise_realm.consumers.name,
        authwise_realm.partner.name,
      ]

      # The identifier screen. kind is left out: that is EMAIL. Write
      # "USERNAME" or "ANY" to change it, but never "KIND_UNSPECIFIED".
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
            realmName    = authwise_realm.partner.name
            providerName = authwise_provider.partner_saml.name
          }
        },
      ]

      # Everyone else. Required. With no providerName, the realm's own
      # selection applies: its password provider here.
      defaultTarget = {
        realmName = authwise_realm.consumers.name
      }
    }
  })
}
