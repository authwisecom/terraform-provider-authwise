# login.acme.com's activation, between its two DNS records.
resource "authwise_domain" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  hostname  = "login.acme.com"
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
