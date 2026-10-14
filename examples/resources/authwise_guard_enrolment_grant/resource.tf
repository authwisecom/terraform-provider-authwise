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
