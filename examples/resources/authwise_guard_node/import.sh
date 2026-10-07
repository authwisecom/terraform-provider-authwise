# Import by full resource name. An imported node has no auth_code:
# guard-control returns it only when the node is created.
terraform import authwise_guard_node.gateway tenants/t-01/networks/n-01/nodes/nd-01
