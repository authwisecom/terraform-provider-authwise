# Identifier-first login

The login page asks for an email address first and routes it: partner
employees go straight to their company's SAML IdP, and everyone else gets
the consumer realm's password page. The routing lives on the issuer, in
`config.multiRealmProviderSelector` (apis v0.11.0).

See [`docs/resources/issuer.md`](../../docs/resources/issuer.md) for the
selector's fields, and for the rules kit refuses a selector over.

`partner-idp-signing.crt` is a placeholder, so the example validates as it
is. Replace it with the certificate the partner published.
