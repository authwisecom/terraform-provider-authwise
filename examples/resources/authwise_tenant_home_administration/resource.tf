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

# A person administers the tenant's Guard. In the administration audience,
# kit requires a user's or a group's binding of guardcontrol.tenantAdmin or
# guardcontrol.networkAdmin to carry the step-up condition and its anchor.
# authwise_guard_enrolment_grant writes the same binding with less to say.
resource "authwise_access_binding" "ada_guard_admin" {
  issuer_id     = authwise_tenant_home_administration.this.issuer_id
  audience_id   = authwise_tenant_home_administration.this.audience_id
  subject_type  = "user"
  subject_id    = "u-01"
  role_name     = "guardcontrol.tenantAdmin"
  resource_type = "guardcontrol.tenant"
  resource_id   = authwise_tenant_home_administration.this.tenant_id
  condition_id  = authwise_tenant_home_administration.this.step_up_condition_id
}
