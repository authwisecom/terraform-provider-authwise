# Social and enterprise sign-in

One realm offering Google, Microsoft, GitHub, Apple, an OIDC provider
(Okta) and a plain OAuth 2.0 provider (Discord). Requires Terraform 1.11 or
later, because the secrets are write-only arguments.

Each provider follows the same pattern:

1. The client secret goes into an `authwise_secret` through `payload_wo`,
   so it is never in the plan, the state or any API response. For Apple,
   the secret is the `.p8` key.
2. The provider's config data source names that secret by reference
   (`client_secret_ref`, or `private_key_ref` for Apple).
3. An `authwise_provider` takes the data source's `any` output.

Facebook, LinkedIn, Okta and Auth0 have their own data sources
(`authwise_provider_facebook`, `_linkedin`, `_okta`, `_auth0`) and follow
the same pattern. For Okta and Auth0, `issuer` is the authorization
server's issuer URL.

## What kit refuses

- A `config` of another type's message: `provider_type` must match the
  data source.
- An `authorization_params` key that kit sets itself: `client_id`,
  `redirect_uri`, `response_type`, `scope`, `state`, `nonce`,
  `code_challenge`, `code_challenge_method` and `access_type`, matched
  without case. Use `scopes` for scopes.
- On `oauth`, an `identifier_source` that is not `userinfo.<path>`, or an
  empty `claim_map`. This type has no id_token to fall back on.
- A `claim_map` that is not valid for the type's protocol.
