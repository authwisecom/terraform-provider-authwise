# Import by full resource name.
terraform import authwise_domain.login tenants/t-01/domains/d-01

# Or by the hostname in place of the id, as state written by 0.18 and
# earlier named it; the import resolves it to the domain's id.
terraform import authwise_domain.main tenants/t-01/domains/acme.id.authwise.com
