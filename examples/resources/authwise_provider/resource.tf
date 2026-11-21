# Realm-scoped: the provider people sign in with, its config built by a
# config data source and its client secret held by reference.
resource "authwise_secret" "google" {
  display_name = "Google client secret"
  payload_wo   = var.google_client_secret
}

data "authwise_provider_google" "this" {
  client_id         = "123456789012-abc.apps.googleusercontent.com"
  client_secret_ref = { name = authwise_secret.google.name }
}

resource "authwise_provider" "google" {
  realm_id      = authwise_realm.employees.realm_id
  display_name  = "Sign in with Google"
  provider_type = "google"
  config        = data.authwise_provider_google.this.any
}

# Your workforce directory over SAML: Entra ID, which is the record for its
# own domains (kit 1.98.0 or later). The list is what makes an asserted
# address count as verified, and only for those domains: the tenant's guest
# accounts carry addresses that belong to someone else.
data "authwise_provider_saml" "entra" {
  idp_entity_id              = "https://sts.windows.net/00000000-0000-0000-0000-000000000000/"
  idp_sso_url                = "https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/saml2"
  idp_signing_certificate_id = authwise_certificate.entra_signing.certificate_id
}

resource "authwise_provider" "entra" {
  realm_id      = authwise_realm.employees.realm_id
  display_name  = "Contoso staff"
  provider_type = "saml"
  config        = data.authwise_provider_saml.entra.any

  trusted_email_domains         = ["contoso.com"]
  trust_upstream_email_verified = true
  link_by_verified_email        = true

  # Entra states MFA in an attribute, not in the class reference. The value
  # is the name of the realm's two-factor acr level.
  trust_upstream_amr = true
  upstream_acr_map = {
    "attribute:http://schemas.microsoft.com/claims/authnmethodsreferences=http://schemas.microsoft.com/claims/multipleauthn" = "urn:authwise:acr:2"
  }
}
