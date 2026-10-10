# Declare Guard's tenant type under kit's tenant: a binding anchored at an
# Authwise tenant then reaches every Guard tenant linked under it.
resource "authwise_access_resource_type" "guardcontrol_tenant" {
  access_resource_type_id = "guardcontrol.tenant"
  parent_type             = "tenant"
  expansion_mode          = "PUSH"
  description             = "A Guard tenant, administered through its parent Authwise tenant"
}
