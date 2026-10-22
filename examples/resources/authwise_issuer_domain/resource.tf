# login.acme.com serves the issuer, with the DNS records on Cloudflare in
# the same apply. The records are keyed on purpose, which is known at plan;
# keying them on dns_records' own values would fail, because those are
# known only after the domain is created.
resource "authwise_issuer_domain" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  hostname  = "login.acme.com"
}

resource "cloudflare_record" "authwise" {
  for_each = toset(["traffic", "challenge"])

  zone_id = var.zone_id
  name    = one([for r in authwise_issuer_domain.login.dns_records : r.name if r.purpose == each.key])
  type    = one([for r in authwise_issuer_domain.login.dns_records : r.type if r.purpose == each.key])
  content = one([for r in authwise_issuer_domain.login.dns_records : r.value if r.purpose == each.key])

  # DNS-only. Proxied, Cloudflare would stand in front of Authwise, which
  # is not supported, and the certificate authority could not see the
  # challenge record.
  proxied = false
}
