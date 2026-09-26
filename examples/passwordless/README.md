# Passwordless sign-in

Two realms, two passwordless primaries:

- **Customers** can sign in with a password or with a **magic link**: one
  email carrying a link and a short code. The link works only in the
  browser that asked for it, and the code is typed into that same tab.
  Sign-up follows the realm's self-signup setting, as it does for
  passwords.
- **Workforce** signs in with **passkeys** alone.

## Magic link

`authwise_provider_magic_link` builds the config. Every field is optional:

| field | default | notes |
| --- | --- | --- |
| `code_length` | 6 | 6 to 8 digits; the attempt cap, not the length, keeps a short code safe |
| `ttl` | `"10m"` | a duration (`"600s"`, `"10m"`); how long the link and the code live |
| `mode` | `LINK_AND_CODE` | `CODE_ONLY` for people who read mail on one device and sign in on another |
| `identifier_attribute` | `email` | the only value kit supports |

## Passkeys

The passkey provider has no settings of its own:
`authwise_provider_passkey` exists for its `any` output, so a plan never
spells the type URL by hand. It uses the relying party of the realm's
`webauthn` factor, meaning the same credentials and the same `rp_id`.
**Without an enabled webauthn factor it signs nobody in.** kit refuses the
first sign-in with "realm offers no webauthn factor". Keep the factor's
`resident_key` at `preferred` or `required`, so that a credential enrolled
as a second factor can also sign in as a passkey.
