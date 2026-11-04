# Every login goes to one realm.
data "authwise_issuer_config" "this" {
  access_token_expire_seconds = 900

  single_realm_provider_selector = {
    realm_name = authwise_realm.employees.name
  }

  cors = {
    allowed_origins = ["https://app.example.com"]
  }
}

resource "authwise_domain" "this" {
  hostname = "login.example.com"
}

resource "authwise_issuer" "this" {
  domain_id = authwise_domain.this.domain_id
  config    = data.authwise_issuer_config.this.json
}

# An issuer that accepts Claude's client ID metadata documents: a client_id
# that is an https URL on claude.ai is fetched and registered, any other host
# is refused. To accept a document from any https host instead, the
# deliberate open setting, leave trusted_domains out and set
# allow_any_host = true.
data "authwise_issuer_config" "mcp" {
  cimd = {
    enabled         = true
    trusted_domains = ["claude.ai"]
  }
}

resource "authwise_issuer" "mcp" {
  domain_id = authwise_domain.this.domain_id
  config    = data.authwise_issuer_config.mcp.json
}
