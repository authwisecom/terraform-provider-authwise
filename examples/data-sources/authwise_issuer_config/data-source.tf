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

resource "authwise_issuer" "this" {
  domain_name = "login.example.com"
  config      = data.authwise_issuer_config.this.json
}
