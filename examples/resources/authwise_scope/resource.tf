# A scope that puts permissions in the token when a client asks for it.
# A client that requests "users.read" gets, in the `permissions` claim, the
# permissions the subject holds among those associated below; never every
# permission the subject holds. kind is one of the values listed under Schema.
resource "authwise_scope" "users_read" {
  scope_id = "users.read"
  kind     = "authorization.permissionGroups"
}

# kit reads this set only for a scope of kind authorization.permissionGroups.
resource "authwise_scope_access_permissions" "users_read" {
  scope              = authwise_scope.users_read.name
  access_permissions = [authwise_access_permission.users_read.name]
}
