# Import by the trust's full name. An uploaded jwks is never read back, so
# the first apply after an import uploads the configured document again.
terraform import authwise_guard_cluster_trust.gfk tenants/t-01/networks/n-01/clusterTrusts/ct-01
