# A native host: the issuer's own Authwise hostname.
resource "authwise_domain" "main" {
  hostname = "acme.id.authwise.com"
}

resource "authwise_issuer" "main" {
  domain_id = authwise_domain.main.domain_id
}

# A custom host: login.acme.com serves the issuer, with its DNS records on
# Cloudflare, in one apply that follows "challenge first, traffic after
# PROVISIONING": the challenge record, then the activation waiting for the
# certificate, then the traffic record.
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
