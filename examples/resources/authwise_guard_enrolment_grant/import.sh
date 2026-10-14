# Import by the Guard tenant's id and the binding's name at the home,
# joined by a slash. Only a grant at the Guard tenant itself can be imported.
terraform import authwise_guard_enrolment_grant.ada gt-01/audiences/a-01/access-bindings/ab-01
