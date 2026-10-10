# A Guard network

A Guard tenant, a network in it, a relay, the two things on the far side of
the network people reach (an office subnet and an internal application), a
gateway server that routes to the subnet, and an invite that admits one
person with both granted.

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
# Edit provider.tf (endpoints, scope AWIDs), the locals at the top of
# main.tf and the tenant's users link, then:
terraform init
terraform apply
terraform output -raw invite_url          # send it to the person you invited
terraform output -raw gateway_auth_code   # enrol the gateway with it
```

## What terraform does not own

- **People's nodes.** A person's node joins by following an invite. A
  server's node is declared (`authwise_guard_node`) and enrols with its
  auth code; what it may reach is `authwise_guard_node_grants`.
- **People.** Guard's users mirror the people who joined.

Both matter on destroy: guard-control refuses to delete a network that still
has nodes, and to delete a tenant that still has networks or users.

## Invites

The invite's `code` and `url` are shown once, when it is created, and kept
in state, so treat the state as sensitive. A spent or expired invite stays
in state and plans clean; to issue another, replace it:

```sh
terraform apply -replace=authwise_guard_invite.dana
```
