# Import by full resource name. The secret cannot be recovered: an imported
# client secret has no `secret` until a keepers change mints a new one.
terraform import authwise_client_secret.example tenants/t-01/issuers/i-01/clients/c-01/client-secrets/cs-01
