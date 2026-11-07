# The tenant's administrators sign in at its issuer on login.acme.com, and
# https://login.acme.com/admin/ serves the administration console. Run with
# the tenant owner's credentials: only tenancy.owner may enable it.
resource "authwise_domain" "login" {
  custom   = true
  hostname = "login.acme.com"
}

resource "authwise_issuer" "home" {}

# Home administration needs a binding at the root of a host.
resource "authwise_issuer_domain_binding" "home" {
  issuer_id = authwise_issuer.home.issuer_id
  domain_id = authwise_domain.login.domain_id
}

resource "authwise_tenant_home_administration" "this" {
  issuer_id = authwise_issuer.home.issuer_id

  depends_on = [authwise_issuer_domain_binding.home]
}
