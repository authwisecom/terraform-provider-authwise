# Authentication and secrets

A workforce realm's second-step authentication: factors, the Duo client
secret held by reference, the realm's authentication policy, and a `check`
over kit's warnings. Requires Terraform 1.11 or later (write-only
arguments) and a kit build carrying kit#544.

## Where things live

- **Secret material** goes in through `payload_wo` and nowhere else: it is
  not in the plan, the state, or any API response. Rotate by changing the
  material and bumping `payload_wo_version`; changing the material alone
  does nothing, since Terraform keeps nothing to compare it with.
- **The policy** is `authwise_realm_authentication_policy`, not part of
  `authwise_realm`'s `config` — the realm resource leaves that part of its
  config alone and refuses an `authentication` key in it.

## Warnings

kit accepts some writes it doubts — a rule requiring a factor type no
active factor offers can never be met — and answers them with a warning.
The provider shows those as Terraform warnings on the apply that caused
them, and the `check` block shows the realm's current warnings on every
plan. Try `terraform apply -var passkeys_status=disabled`: the admins rule
now requires a factor nobody can use.

## Not Terraform's job

Revoking a person's authenticators or remembered devices is an operational
act, not configuration: use `awctl`.
