# SAML 2.0 in both roles, as Terraform.
#
# Authwise speaks SAML in two directions and they are separate connections
# with separate trust:
#
#   SP role  — Authwise consumes a partner IdP, so users log in to Authwise
#              with the partner's credentials. Configured on a *provider*.
#   IdP role — Authwise issues assertions to a partner SP, so their app logs
#              its users in with Authwise. Configured on a *client*.
#
# Every certificate reference below is a Certificate resource name. Inbound
# signatures are checked against that certificate and nothing else: the
# KeyInfo a partner puts in an assertion is never a source of trust, so the
# anchor has to be a row someone deliberately created.
#
# Metadata import and export are not Terraform operations — see README.md.

locals {
  # The realm whose users log in through the partner IdP.
  realm_id = "r-01"

  # What the partner calls us, and what we call them. Both sides agree these
  # out of band, or read them off each other's metadata.
  our_sp_entity_id = "https://login.example.com/saml/sp"
}

# ---------------------------------------------------------------------------
# SP role: log our users in through a partner IdP.
# ---------------------------------------------------------------------------

# The partner's signing certificate, handed over out of band. A non-empty
# import_certificate_pem imports; leaving it empty mints. There is no
# separate import RPC — the field is the switch.
#
# It is create-only: a partner who rotates gets a new Certificate row, and
# the provider is repointed at it. That is deliberate — replacing the trust
# anchor under a live connection should be a visible act.
resource "authwise_certificate" "partner_idp_signing" {
  display_name           = "Partner IdP signing"
  use                    = "CERTIFICATE_USE_SIGNING"
  import_certificate_pem = file("${path.module}/partner-idp-signing.crt")

  labels = {
    partner = "partner-co"
  }
}

# Ours: what we sign AuthnRequests with, and decrypt encrypted assertions
# with. Minted here — no PEM in, so kit generates the key pair and answers
# with what it actually produced (subject, not_before, not_after).
#
# The mint parameters are input-only: kit consumes them on create and never
# returns them, so they stay in state exactly as written here and never
# appear in a refresh. An imported resource has no value for them.
resource "authwise_certificate" "sp_signing" {
  display_name        = "Our SP signing key"
  use                 = "CERTIFICATE_USE_SIGNING_AND_ENCRYPTION"
  subject_common_name = "login.example.com"
  validity_days       = 825
  key_size            = 2048
}

data "authwise_provider_saml" "partner" {
  # The partner (the upstream IdP). An assertion from any other issuer is
  # refused even when correctly signed by a certificate we trust.
  idp_entity_id              = "https://idp.partner-co.example/metadata"
  idp_sso_url                = "https://idp.partner-co.example/sso"
  idp_sso_binding            = "HTTP-Redirect"
  idp_signing_certificate_id = authwise_certificate.partner_idp_signing.name

  # Ours.
  signing_certificate_id = authwise_certificate.sp_signing.name
  sign_authn_requests    = true

  # At least one of these must hold: an SP that verifies nothing accepts
  # assertions from anyone.
  want_assertions_signed = true
  want_response_signed   = false

  name_id_format = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
  allow_create   = true

  # Leave false unless the partner's portal is the intended entry point: an
  # unsolicited Response correlates to no request we made, which is the
  # replay and CSRF protection it removes.
  allow_idp_initiated = false

  # Inbound only, per connection, logged on every use. Leave false unless a
  # partner genuinely cannot sign with anything better.
  accept_sha1        = false
  clock_skew_seconds = 120

  # Assertion to User. Empty uses the documented defaults.
  identifier_source = "assertion.name_id"

  claim_map = {
    # target name -> source path
    map = {
      email      = "assertion.attributes.mail"
      given_name = "assertion.attributes.givenName"
      surname    = "assertion.attributes.sn"
    }
    # target name -> literal, for claims that come from nowhere
    static = {
      federation_source = "partner-co"
    }
    # copied through unchanged when present; applied last, never overwrites
    passthrough = ["department"]
  }
}

resource "authwise_provider" "partner" {
  realm_id      = local.realm_id
  display_name  = "Partner Co (SAML)"
  provider_type = "saml"
  config        = data.authwise_provider_saml.partner.any
}

# ---------------------------------------------------------------------------
# IdP role: let a partner's app rely on us.
# ---------------------------------------------------------------------------

# What we sign assertions with. Required — an IdP that signs nothing issues
# assertions anyone can forge.
resource "authwise_certificate" "idp_signing" {
  display_name        = "Our IdP signing key"
  use                 = "CERTIFICATE_USE_SIGNING"
  subject_common_name = "login.example.com"
  validity_days       = 825
}

# The relying party's signing certificate. Only needed if you require them
# to sign their AuthnRequests.
resource "authwise_certificate" "relying_party_signing" {
  display_name           = "Relying party signing"
  use                    = "CERTIFICATE_USE_SIGNING"
  import_certificate_pem = file("${path.module}/relying-party-signing.crt")
}

data "authwise_saml_relying_party_config" "app" {
  # The partner (the relying SP).
  sp_entity_id = "https://app.partner-co.example/saml/metadata"

  # Matched exactly. The first is the default: used for an IdP-initiated
  # login and for a request that names no ACS.
  acs_urls = [
    "https://app.partner-co.example/saml/acs",
    "https://app.partner-co.example/saml/acs/alt",
  ]
  acs_binding               = "HTTP-POST"
  sp_signing_certificate_id = authwise_certificate.relying_party_signing.name

  # Ours. At least one of sign_response, sign_assertions or
  # encrypt_assertions must hold.
  signing_certificate_id = authwise_certificate.idp_signing.name
  sign_response          = true
  sign_assertions        = true
  encrypt_assertions     = false

  # Off by default because most SPs do not sign, and the request carries
  # nothing an unsigned one could abuse that the ACS allow-list does not
  # already close.
  want_authn_requests_signed = true

  # Issuing an assertion for a destination nobody asked about needs a
  # default ACS and a deliberate decision.
  allow_idp_initiated = false

  name_id_format = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
  name_id_source = "user.email"

  assertion_lifetime_seconds      = 300
  session_not_on_or_after_seconds = 3600

  # Grant and user to AttributeStatement. Empty issues nothing but the
  # NameID, which is what a partner asking only "who is this" needs.
  claim_map = {
    map = {
      email  = "user.email"
      name   = "user.display_name"
      tenant = "grant.tenant_id"
    }
    passthrough = ["groups"]
  }
}

resource "authwise_client" "partner_app" {
  display_name = "Partner Co app (SAML)"
  audience_id  = "a-01"
  grant_type   = "authorization_code"
  config       = data.authwise_saml_relying_party_config.app.any
}

output "sp_entity_id" {
  description = "Our entity id as an SP; the partner IdP needs it to build their side."
  value       = local.our_sp_entity_id
}

output "provider_name" {
  description = "Feed this to awctl export-saml-metadata to render the document for the partner."
  value       = authwise_provider.partner.name
}

output "client_name" {
  description = "Feed this to awctl export-saml-metadata to render our IdP document."
  value       = authwise_client.partner_app.name
}
