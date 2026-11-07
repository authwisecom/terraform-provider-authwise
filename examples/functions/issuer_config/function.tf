# Every login goes to one realm. Evaluated at plan, so a change to the
# realm does not plan an issuer update.
resource "authwise_issuer" "this" {
  config = provider::authwise::issuer_config({
    access_token_expire_seconds = 900

    single_realm_provider_selector = {
      realm_name = authwise_realm.employees.name
    }

    cors = {
      allowed_origins = ["https://app.example.com"]
    }
  })
}

# An issuer that accepts Claude's client ID metadata documents: a client_id
# that is an https URL on claude.ai is fetched and registered, any other host
# is refused. To accept a document from any https host instead, the
# deliberate open setting, leave trusted_domains out and set
# allow_any_host = true.
resource "authwise_issuer" "mcp" {
  config = provider::authwise::issuer_config({
    cimd = {
      enabled         = true
      trusted_domains = ["claude.ai"]
    }
  })
}

# Each issuer at the root of its own host: one host and path serve one
# issuer.
resource "authwise_issuer_domain_binding" "this" {
  issuer_id = authwise_issuer.this.issuer_id
  domain_id = authwise_domain.this.domain_id
}

resource "authwise_issuer_domain_binding" "mcp" {
  issuer_id = authwise_issuer.mcp.issuer_id
  domain_id = authwise_domain.mcp.domain_id
}
