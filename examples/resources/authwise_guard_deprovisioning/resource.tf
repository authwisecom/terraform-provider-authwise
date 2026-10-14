# The account the sweep re-checks the users-link home with: a service
# account of that tenant holding tenant.viewer there.
resource "authwise_guard_deprovisioning" "this" {
  tenant_id                = "gt-01"
  client_id                = authwise_service_account.guard_sweep.client_id
  client_secret_wo         = authwise_service_account_secret.guard_sweep.secret
  client_secret_wo_version = 1
  token_url                = "https://id.example.authwise.io/oauth2/token"
  audience                 = "https://api.example.authwise.io"
}
