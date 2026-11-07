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
  config = data.authwise_issuer_config.this.json
}

resource "authwise_issuer_domain_binding" "this" {
  issuer_id = authwise_issuer.this.issuer_id
  domain_id = authwise_domain.this.domain_id
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

# Its own host: one host and path serve one issuer.
resource "authwise_domain" "mcp" {
  hostname = "mcp.example.com"
}

resource "authwise_issuer" "mcp" {
  config = data.authwise_issuer_config.mcp.json
}

resource "authwise_issuer_domain_binding" "mcp" {
  issuer_id = authwise_issuer.mcp.issuer_id
  domain_id = authwise_domain.mcp.domain_id
}
