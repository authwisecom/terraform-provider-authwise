# Guard relay pools

Estate relay pools are named sets of relays that the operator runs and
shares across tenants' networks. A network uses at most one pool. This
example has two pools, a relay in one of them with its credential, a tenant
allowed the second pool, and a network on it.

The pools, their relays and each tenant's access belong to the operator:
guard-control admits them only for the estate's credential, and refuses a
tenant's. A tenant picks among the pools it is allowed
(`authwise_guard_allowed_relay_pools`) through its networks' `relay_pool`,
which takes the pool's name (`relay-pools/rp-…`). guard-control refuses a
slug.
Normally these are two roots with two credentials. This example uses one.

```sh
terraform init
terraform apply
terraform output -raw us_central_a_relay_token   # the relay credential, grk_…
```

## The two defaults

- **Estate default.** This is the pool whose `default` is set. Every tenant
  may use it, and it is the default for any tenant that has none of its own.
  Setting `default` on one pool clears it on the previous one.
- **Tenant default.** This is `authwise_guard_relay_pool_access.default_pool`.
  A network of the tenant created without `relay_pool` gets this pool. The
  network stores it, so changing a default later never moves an existing
  network.

## Relay credentials

`authwise_guard_relay_credential` issues the `grk_…` bearer a relay reports
to guard-control with. It works for a pool relay, and also for a network's
own relay that polls control for its networks. guard-control shows the
credential once, so it is kept in state. Treat the state as sensitive.

- **Rotate** by changing `rotation`. The resource is replaced and a new
  credential issued. The old one keeps working until the relay first uses
  the new one.
- **Destroy revokes nothing.** Revoking is an operator's action on a
  compromised relay, done in awctl or the console. Deleting the relay also
  revokes its credential. After a revoke, the next refresh drops the
  credential from state and the next apply issues a new one.
- **No import.** Importing would have to issue a new credential, so it is
  not supported.

The relay VM itself is outside this provider. Customers run their own relay
VMs, as the Guard documentation describes. Authwise's own pool relays are
built by the `guard-relay` module in authwise-gcp, an internal repository,
with `role = "pool"`. That module reads the credential from Secret Manager,
so store it there:

```hcl
resource "google_secret_manager_secret_version" "us_central_a_relay" {
  secret      = google_secret_manager_secret.us_central_a_relay.id
  secret_data = authwise_guard_relay_credential.us_central_a.credential
}

module "relay_us_central_a" {
  source = "git::https://gitlab.authwise.io/authwise/authwise-gcp.git//terraform/modules/guard-relay?ref=<tag>"

  role     = "pool"
  relay_id = authwise_guard_pool_relay.us_central_a.guard_pool_relay_id
  # The credential's secret, cache_file and the VM's placement: see the
  # module.
}
```

## Destroy order

guard-control refuses to delete a pool that is still in use
(`RELAY_POOL_IN_USE`). Take it off networks and out of tenants' access
first. When `relay_pool` and `allowed_pools` reference the pool
(`authwise_guard_relay_pool.<name>.name`), Terraform destroys them before
the pool. A network on the tenant's default pool, with `relay_pool` unset,
has no reference to the pool, so give it a `depends_on`.
