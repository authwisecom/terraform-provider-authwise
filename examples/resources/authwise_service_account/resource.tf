# A service account for CI: a machine principal the tenant owns. Its roles
# are what it may do at the tenant; the tenant's administrators set them,
# from tenant.admin, tenant.viewer and guardcontrol.admin. Service accounts
# are served at central only.
resource "authwise_service_account" "ci" {
  display_name = "terraform"
  roles        = ["tenant.admin"]
}
