# The token Entra ID authenticates with, valid for a year. The secret is
# kept in state: protect the state as you would the token.
resource "authwise_scim_token" "entra" {
  realm_id           = authwise_realm.staff.realm_id
  scim_connection_id = authwise_scim_connection.entra.scim_connection_id
  expires_at         = "2027-11-16T00:00:00Z"
}

# What to paste into Entra ID as the Secret Token.
output "scim_secret" {
  value     = authwise_scim_token.entra.secret
  sensitive = true
}
