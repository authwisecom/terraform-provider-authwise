# Endpoints

An `authwise_endpoint` is a service kit calls out to — a risk engine, an
external factor, a ledger — described once and referenced by name. Each
directory here applies on its own:

| example | transport | kit authenticates with | kit verifies the service with |
| --- | --- | --- | --- |
| [`bearer`](bearer) | REST | a static token in an `authwise_secret` | the system roots |
| [`kit_token`](kit_token) | gRPC | a token kit signs from an `authwise_issuer` | the system roots, as `server_name` |
| [`mtls`](mtls) | gRPC | a client certificate from an `authwise_certificate` | a private CA, `ca_pem` |

## Transport and TLS

- **REST** is the default, so `endpoint_type` can be left unset. The `address` is an
  absolute `http(s)://` URL, and its scheme decides whether the connection
  uses TLS.
- **gRPC** takes `host:port` or `dns:///host:port`. `insecure = true` means
  **plaintext**: no TLS at all, not "skip verification". It applies to gRPC
  only: kit refuses it on a REST endpoint, and refuses `tls` on an insecure
  one.
- `tls.insecure_skip_verify` is the "accept any certificate" switch, for
  development only.
- kit refuses a loopback, private, link-local or unique-local destination
  unless the install allows private addresses.

## `auth`

`auth` is the protojson encoding of `EndpointAuth`, written with
`jsonencode`. Its keys are **lowerCamel** (`kitToken`, not `kit_token`),
because the value kit returns is compared with the one you wrote. It holds
exactly one scheme:

```hcl
# Authorization: Bearer <secret>
auth = jsonencode({ bearer = { token = { name = authwise_secret.t.name } } })

# HTTP basic; only the password is secret.
auth = jsonencode({ basic = { username = "kit", password = { name = authwise_secret.p.name } } })

# Any header, lower-cased on the wire.
auth = jsonencode({ header = { name = "x-api-key", value = { name = authwise_secret.k.name } } })

# A token kit signs; nothing stored.
auth = jsonencode({ kitToken = { issuer = authwise_issuer.i.name, audience = "https://svc.example.com" } })
```

A secret-backed scheme always names a secret; the endpoint never holds a
credential. Setting one needs `identity.secrets.use`. The client
certificate for mTLS is not an `auth` scheme: it sits in
`tls.client_certificate`, so it can be combined with any of these.

## Deleting

kit refuses to delete an endpoint while anything names it, and the error
names the holder. It also refuses to delete a certificate or an issuer
while an endpoint uses it. Name the endpoint by reference
(`authwise_endpoint.x.name`, even inside `jsonencode`), and Terraform
destroys the holder first. A literal name string gives Terraform no
ordering edge, so add `depends_on` to the holder or replace the literal
with a reference.

## `timeout`

`timeout` is the deadline for one call, used when the caller sets none. It
must be between 100 ms and 60 s. It accepts Go duration syntax (`"5s"`,
`"1.5s"`, `"500ms"`, `"1m30s"`) and keeps the spelling you wrote. An
imported endpoint reads it in protojson form (`"90s"`). When unset, the
install's default of 5 s applies.
