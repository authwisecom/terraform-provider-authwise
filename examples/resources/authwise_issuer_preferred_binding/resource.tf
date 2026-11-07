resource "authwise_issuer" "login" {}

resource "authwise_issuer_domain_binding" "native" {
  issuer_id = authwise_issuer.login.issuer_id
  domain_id = authwise_domain.native.domain_id
}

# The custom domain the applications sign in on.
resource "authwise_issuer_domain_binding" "custom" {
  issuer_id = authwise_issuer.login.issuer_id
  domain_id = authwise_domain.custom.domain_id
}

# Mailed links (an invitation's accept, a password reset) use the custom
# host, so the person ends up signed in where the application looks.
resource "authwise_issuer_preferred_binding" "login" {
  issuer     = authwise_issuer.login.name
  binding_id = authwise_issuer_domain_binding.custom.issuer_domain_binding_id

  # Optional: switch only once the host serves. kit builds links on the
  # first routable binding until the custom domain is ACTIVE either way.
  depends_on = [authwise_domain_activation.custom]
}
