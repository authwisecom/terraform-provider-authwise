# Everyone in the engineering group may enroll devices in the tenant's Guard.
# The group is in one of the tenant's realms.
resource "authwise_guard_enrolment_grant" "engineering" {
  tenant_id    = "t-01"
  subject_type = "group"
  subject_id   = "g-01"
}

# One person, by their user id.
resource "authwise_guard_enrolment_grant" "ada" {
  tenant_id    = "t-01"
  subject_type = "user"
  subject_id   = "u-01"
}

# A group of the tenant's own people administers the tenant's Guard. The
# role is bound in the tenant's administration audience, under its step-up
# condition, so the tenant needs home administration first
# (authwise_tenant_home_administration); without it guard-control refuses the
# grant. The tenant's central administrators need no grant: kit gives them
# Guard's tenant administrator role from their seat.
resource "authwise_guard_enrolment_grant" "it_admins" {
  tenant_id    = "t-01"
  role         = "guardcontrol.tenantAdmin"
  subject_type = "group"
  subject_id   = "g-02"
}

# One person administers one network.
resource "authwise_guard_enrolment_grant" "office_admin" {
  tenant_id    = "t-01"
  network_id   = "n-01"
  role         = "guardcontrol.networkAdmin"
  subject_type = "user"
  subject_id   = "u-02"
}
