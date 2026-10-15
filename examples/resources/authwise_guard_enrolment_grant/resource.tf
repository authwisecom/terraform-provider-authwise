# Everyone in the engineering group may enrol devices in the Guard tenant.
# The group is in one of the Guard tenant's realms at the users-link home.
resource "authwise_guard_enrolment_grant" "engineering" {
  tenant_id    = "gt-01"
  subject_type = "group"
  subject_id   = "g-01"
}

# One person, by their user id at the home.
resource "authwise_guard_enrolment_grant" "ada" {
  tenant_id    = "gt-01"
  subject_type = "user"
  subject_id   = "u-01"
}

# A group of the users-link tenant administers the Guard tenant from its own
# home. The home must advertise a step-up condition for administrative roles.
resource "authwise_guard_enrolment_grant" "it_admins" {
  tenant_id    = "gt-01"
  role         = "guardcontrol.tenantAdmin"
  subject_type = "group"
  subject_id   = "g-02"
}

# One person administers one network.
resource "authwise_guard_enrolment_grant" "office_admin" {
  tenant_id    = "gt-01"
  network_id   = "n-01"
  role         = "guardcontrol.networkAdmin"
  subject_type = "user"
  subject_id   = "u-02"
}
