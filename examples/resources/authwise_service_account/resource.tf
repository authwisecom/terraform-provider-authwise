# A service account for CI: a machine principal the tenant owns. It starts
# with a member seat at the tenant and no roles; bind one with
# authwise_access_binding. Service accounts are served at central only.
resource "authwise_service_account" "ci" {
  display_name = "terraform"
}

# Its subject in Access: subject_type "client", subject_id its client_id.
# Referencing it by expression lets Terraform remove the binding before the
# account, which kit refuses to delete while a binding names it.
resource "authwise_access_binding" "ci_admin" {
  tenant_id     = "t-01"
  issuer_id     = "i-01"
  audience_id   = "a-01"
  subject_type  = "client"
  subject_id    = authwise_service_account.ci.client_id
  role_name     = "tenant.admin"
  resource_type = "tenant"
  resource_id   = authwise_service_account.ci.tenant_id
}
