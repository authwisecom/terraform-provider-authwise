# A native host: an Authwise hostname of the tenant. The issuer answers at
# its root through a binding.
resource "authwise_domain" "main" {
  hostname = "acme.id.authwise.com"
}

resource "authwise_issuer" "main" {}

resource "authwise_issuer_domain_binding" "main" {
  issuer_id = authwise_issuer.main.issuer_id
  domain_id = authwise_domain.main.domain_id
}

# A custom host: login.acme.com, with its DNS records on Cloudflare, in one
# apply that follows "challenge first, traffic after PROVISIONING": the
# challenge record, then the activation waiting for the certificate, then
# the traffic record. The issuer is bound to it as well, so it answers at
# both hosts, each its own iss.
resource "authwise_domain" "login" {
  custom   = true
  hostname = "login.acme.com"
}

resource "authwise_issuer_domain_binding" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  domain_id = authwise_domain.login.domain_id
}

locals {
  login_records = { for r in authwise_domain.login.dns_records : r.purpose => r }
}

resource "cloudflare_record" "login_challenge" {
  zone_id = var.zone_id
  name    = local.login_records["challenge"].name
  type    = local.login_records["challenge"].type
  content = local.login_records["challenge"].value

  # DNS-only. Proxied, Cloudflare would stand in front of Authwise, which
  # is not supported, and the certificate authority could not see the
  # challenge record.
  proxied = false
}

resource "authwise_domain_activation" "login" {
  domain     = authwise_domain.login.name
  depends_on = [cloudflare_record.login_challenge]
}

resource "cloudflare_record" "login_traffic" {
  zone_id = var.zone_id
  name    = local.login_records["traffic"].name
  type    = local.login_records["traffic"].type
  content = local.login_records["traffic"].value
  proxied = false

  depends_on = [authwise_domain_activation.login]
}

# A custom Guard address: guard.acme.com is what the tenant's people type
# into the Guard client, in place of <slug>.guard.<estate>. The same
# records, in the same order; its traffic record points at the tenant's
# default Guard address, so the tenant needs a slug first. No issuer is
# bound to it.
resource "authwise_domain" "guard" {
  purpose  = "guard"
  hostname = "guard.acme.com"
}

locals {
  guard_records = { for r in authwise_domain.guard.dns_records : r.purpose => r }
}

resource "cloudflare_record" "guard_challenge" {
  zone_id = var.zone_id
  name    = local.guard_records["challenge"].name
  type    = local.guard_records["challenge"].type
  content = local.guard_records["challenge"].value
  proxied = false
}

resource "authwise_domain_activation" "guard" {
  domain     = authwise_domain.guard.name
  depends_on = [cloudflare_record.guard_challenge]
}

resource "cloudflare_record" "guard_traffic" {
  zone_id = var.zone_id
  name    = local.guard_records["traffic"].name
  type    = local.guard_records["traffic"].type
  content = local.guard_records["traffic"].value
  proxied = false

  depends_on = [authwise_domain_activation.guard]
}

# The tenant's own console address: admin.acme.com is where its
# administrators open the Authwise console, signing in at the tenant's own
# issuer, without the tenant's slug in the path. The same records, in the same
# order; its traffic record points at the install's edge, the same target as
# login.acme.com's. No issuer is bound to it, and it is of use once the tenant
# has home administration (authwise_tenant_home_administration).
resource "authwise_domain" "console" {
  purpose  = "console"
  hostname = "admin.acme.com"
}

locals {
  console_records = { for r in authwise_domain.console.dns_records : r.purpose => r }
}

resource "cloudflare_record" "console_challenge" {
  zone_id = var.zone_id
  name    = local.console_records["challenge"].name
  type    = local.console_records["challenge"].type
  content = local.console_records["challenge"].value
  proxied = false
}

resource "authwise_domain_activation" "console" {
  domain     = authwise_domain.console.name
  depends_on = [cloudflare_record.console_challenge]
}

resource "cloudflare_record" "console_traffic" {
  zone_id = var.zone_id
  name    = local.console_records["traffic"].name
  type    = local.console_records["traffic"].type
  content = local.console_records["traffic"].value
  proxied = false

  depends_on = [authwise_domain_activation.console]
}
