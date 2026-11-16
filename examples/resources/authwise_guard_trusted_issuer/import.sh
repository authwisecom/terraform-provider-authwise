# Import by the issuer's full name. An uploaded jwks is never read back, so
# the first apply after an import uploads the configured document again.
terraform import authwise_guard_trusted_issuer.build tenants/t-01/networks/n-01/trustedIssuers/ti-01
