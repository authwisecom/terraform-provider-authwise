# Entra ID pushes the realm's users and groups. Each user's userName becomes
# their identifier under the Entra ID provider, so they sign in through it;
# a user Entra ID deletes is deactivated.
resource "authwise_scim_connection" "entra" {
  realm_id     = authwise_realm.staff.realm_id
  display_name = "Entra ID"
  issuer_id    = authwise_issuer.staff.issuer_id
  provider_id  = authwise_provider.entra.provider_id
}

# What to paste into Entra ID as the Tenant URL.
output "scim_base_url" {
  value = authwise_scim_connection.entra.base_url
}
