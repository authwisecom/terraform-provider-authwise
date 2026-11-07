resource "authwise_domain" "native" {
  hostname = "acme.id.authwise.com"
}

resource "authwise_domain" "login" {
  custom   = true
  hostname = "login.acme.com"
}

# Two issuers, each on its own host: one host and path serve one issuer.
resource "authwise_issuer" "staff" {}

resource "authwise_issuer" "customers" {}

resource "authwise_issuer_domain_binding" "staff" {
  issuer_id = authwise_issuer.staff.issuer_id
  domain_id = authwise_domain.native.domain_id
}

resource "authwise_issuer_domain_binding" "customers" {
  issuer_id = authwise_issuer.customers.issuer_id
  domain_id = authwise_domain.login.domain_id
}

output "customers_issuer_url" {
  value = authwise_issuer_domain_binding.customers.url # https://login.acme.com
}
