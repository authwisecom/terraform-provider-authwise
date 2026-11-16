# Import by full resource name. The secret cannot be recovered: an imported
# token has no `secret` until a `keepers` change replaces it with a new one.
terraform import authwise_scim_token.entra tenants/t-01/realms/r-01/scim-tokens/st-01
