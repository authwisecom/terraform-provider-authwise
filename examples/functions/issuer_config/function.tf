# Every login goes to one realm. Evaluated at plan, so a change to the
# realm does not plan an issuer update.
resource "authwise_issuer" "this" {
  domain_id = authwise_domain.this.domain_id
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
