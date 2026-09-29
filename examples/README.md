# Examples

There are two kinds of example here.

**Documentation snippets.** tfplugindocs embeds these into `docs/`:

- `provider/provider.tf` on the provider page;
- `resources/<resource>/resource.tf` as a resource's Example Usage;
- `resources/<resource>/import.sh` as its Import section.

Run `make docs` after changing any of them. CI fails when `docs/` is out of
date.

**Worked configurations.** Each of these applies on its own. The
acceptance suite (`make testacc`) applies every one listed here against an
in-memory kit stub and requires a clean re-plan:

| directory | what it shows |
| --- | --- |
| `authn` | factors, the realm authentication policy, a secret by reference |
| `endpoints/{bearer,kit_token,mtls}` | outbound endpoints and their auth schemes |
| `guard-catalog` | an Access catalog: permissions, a role, a binding, a client |
| `identifier-first` | routing logins between realms by email domain |
| `passwordless` | magic link and passkeys |
| `social` | Google, Microsoft, GitHub, Apple, OIDC and OAuth sign-in |

`saml` and `simple` are not applied by the suite.
