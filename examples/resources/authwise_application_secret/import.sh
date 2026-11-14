# Import by full resource name. The secret cannot be recovered: an imported
# application secret has no `secret` until a keepers change creates a new one.
terraform import authwise_application_secret.example tenants/t-01/issuers/i-01/applications/c-01/application-secrets/cs-01
