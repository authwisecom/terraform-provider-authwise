# Import by the kit tenant's id and the binding's name in kit,
# joined by a slash. The role and the network are read from guard-control.
# Against a guard-control without role grants (guard-control#59), only an
# enrollment grant at the tenant itself can be imported.
terraform import authwise_guard_enrolment_grant.ada t-01/audiences/a-01/access-bindings/ab-01
