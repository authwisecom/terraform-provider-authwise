# A scope that puts the subject's permissions in the token when a client
# asks for it. kind is one of the values listed under Schema.
resource "authwise_scope" "users_read" {
  scope_id = "users.read"
  kind     = "authorization.permissions"
}

resource "authwise_scope_access_permissions" "users_read" {
  scope              = authwise_scope.users_read.name
  access_permissions = [authwise_access_permission.users_read.name]
}
