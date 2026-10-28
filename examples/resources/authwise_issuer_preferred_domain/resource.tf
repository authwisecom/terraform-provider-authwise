resource "authwise_issuer" "login" {
  domain_id = authwise_domain.native.domain_id
}

# The custom domain the applications sign in on, attached to the issuer.
resource "authwise_domain" "custom" {
  hostname  = "auth.example.com"
  issuer_id = authwise_issuer.login.issuer_id
}

# Mailed links (an invitation's accept, a password reset) use the custom
# host, so the person ends up signed in where the application looks.
resource "authwise_issuer_preferred_domain" "login" {
  issuer    = authwise_issuer.login.name
  domain_id = authwise_domain.custom.domain_id

  # Optional: switch only once the host serves. kit keeps the native host
  # for links until the custom domain is ACTIVE either way.
  depends_on = [authwise_domain_activation.custom]
}
