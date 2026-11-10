# A Guard network

A network on a kit tenant Guard is on, a relay, the two things on the far side of
the network people reach (an office subnet and an internal application), and
a gateway server that routes to the subnet.

Guard is managed by the same provider as the identity objects, through a
second endpoint: `guard_endpoint` (or `AUTHWISE_GUARD_ENDPOINT`) is
guard-control's gRPC address, and the provider sends it the same bearer it
sends kit. Without it, every `authwise_guard_*` resource fails at plan.

The credential needs Guard's permissions on the tenant. `../guard-catalog`
creates them, the role carrying them and the console client; it can be
applied in the same configuration as this one (copy its `main.tf` in beside
this `main.tf` under another name), which is how the provider's acceptance
suite applies the two.

```sh
# Edit provider.tf (endpoints, scope AWIDs) and the local at the top of
# main.tf (the kit tenant Guard is on), then:
terraform init
terraform apply
terraform output -raw gateway_auth_code   # enroll the gateway with it
```

## What terraform does not own

- **Guard on the tenant.** There is no Guard tenant: Guard is keyed by the
  kit tenant (`t-…`), passed as `tenant_id`. Turning Guard on for a tenant
  is kit's; until it is, guard-control refuses with `GUARD_NOT_ENABLED`.
  `authwise_guard_tenant_settings` holds Guard's settings for the tenant,
  and destroying it leaves them in place.
- **People's nodes.** A person types the tenant's Guard address into the
  client and signs in; their node joins the networks they may enroll in
  (`authwise_guard_enrolment_grant`). A server's node is declared
  (`authwise_guard_node`) and enrolls with its auth code; what it may reach
  is `authwise_guard_node_grants`.
- **People.** Guard's users mirror the people who joined.

Nodes matter on destroy: guard-control refuses to delete a network that
still has nodes.

## Inviting people

Guard has no invites of its own. Invite a person with kit's invitation
(the console's "Invite a person"), and grant them a network with
`authwise_guard_enrolment_grant`. A custom Guard address such as
`guard.acme.com` is an `authwise_domain` with `product = "guard"`.
