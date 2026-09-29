---
page_title: "authwise_endpoint Resource - Authwise"
subcategory: ""
description: |-
  A service kit calls out to: its transport and address, how kit verifies it (tls), how kit authenticates to it (auth), and the per-call deadline (timeout).
---

# authwise_endpoint (Resource)

A service kit calls out to, such as a risk engine, an external factor or a
ledger. You describe it once, and consumers refer to it by name. An
endpoint sets four things:

- the transport and address;
- how kit verifies the service (`tls`);
- how kit authenticates to it (`auth`, plus `tls.client_certificate` for
  mTLS);
- the deadline for one call (`timeout`).

An endpoint never holds a credential. A static credential lives in an
[`authwise_secret`](secret.md) and is referenced by name. A token kit signs
needs no secret at all.

## Example Usage

### Bearer token from a secret (REST)

```terraform
resource "authwise_secret" "risk_token" {
  display_name       = "Risk service token"
  payload_wo         = var.risk_token
  payload_wo_version = var.risk_token_version
}

resource "authwise_endpoint" "risk" {
  display_name = "Risk service"
  address      = "https://risk.example.com/v1"
  timeout      = "2s"

  auth = jsonencode({
    bearer = {
      token = { name = authwise_secret.risk_token.name }
    }
  })
}
```

### A token kit signs (gRPC)

```terraform
resource "authwise_issuer" "internal" {
  domain_name = "auth.example.com"
  path        = "/internal"
}

resource "authwise_endpoint" "ledger" {
  display_name  = "Ledger"
  endpoint_type = "GRPC"
  address       = "ledger.example.com:443"
  timeout       = "1500ms"

  tls = {
    server_name = "ledger.internal"
  }

  auth = jsonencode({
    kitToken = {
      issuer   = authwise_issuer.internal.name
      audience = "https://ledger.example.com"
    }
  })
}
```

kit signs the token with the issuer's key. It sends it as
`authorization: Bearer <token>`, with `aud` set to `audience`,
`sub: authwise-internal` and a five-minute lifetime. The service verifies it
against the issuer's JWKS.

### Mutual TLS with a certificate kit holds (gRPC)

```terraform
resource "authwise_certificate" "kit_client" {
  display_name        = "kit client (payments)"
  use                 = "CERTIFICATE_USE_SIGNING"
  subject_common_name = "kit.example.com"
  validity_days       = 365
}

resource "authwise_endpoint" "payments" {
  display_name  = "Payments"
  endpoint_type = "GRPC"
  address       = "payments.internal.example.com:8443"

  tls = {
    ca_pem             = file("${path.module}/payments-ca.pem")
    client_certificate = authwise_certificate.kit_client.name
  }
}
```

The three configurations apply as they are from
[`examples/endpoints`](../../examples/endpoints). The acceptance suite runs
them the same way.

## Transport

| `endpoint_type` | `address` | plaintext |
| --- | --- | --- |
| `REST` (the default) | an absolute `http(s)://` URL with no userinfo; the base a consumer appends its path to | an `http://` URL |
| `GRPC` | `host:port` or `dns:///host:port` | `insecure = true` |

`insecure` means **plaintext**: no TLS at all, not "skip verification". It
applies to gRPC only. kit refuses it on a REST endpoint, where the URL's
scheme already says whether the connection is plaintext. kit also refuses
`tls` on an insecure endpoint. To accept any certificate during
development, use `tls.insecure_skip_verify`.

kit refuses a loopback, private, link-local or unique-local destination
unless the install sets `integration.allowPrivateAddresses`.

## `auth`

`auth` is the protojson encoding of `EndpointAuth`, written with
`jsonencode`. Its keys are **lowerCamel**, such as `kitToken` rather than
`kit_token`. The value kit returns is compared with the value you wrote, and
kit returns lowerCamel. It holds exactly one scheme:

| scheme | sends | JSON |
| --- | --- | --- |
| `bearer` | `authorization: Bearer <secret>` | `{ bearer = { token = { name = <secret> } } }` |
| `basic` | HTTP basic; only the password is secret | `{ basic = { username = "kit", password = { name = <secret> } } }` |
| `header` | any header, lower-cased on the wire | `{ header = { name = "x-api-key", value = { name = <secret> } } }` |
| `kitToken` | a token kit signs; nothing stored | `{ kitToken = { issuer = <issuer>, audience = "…" } }` |

`<secret>` is an `authwise_secret`'s `name` and `<issuer>` is an
`authwise_issuer`'s `name`, both of the endpoint's tenant. Referencing them
inside `jsonencode` orders the endpoint after them. Writing a secret-backed
scheme needs `identity.secrets.use`. Leaving `auth` unset means kit presents
nothing.

The client certificate for mTLS is not an `auth` scheme. It sits in
`tls.client_certificate`, so it can be combined with any of the schemes
above.

## `timeout`

`timeout` is the deadline for one call, used when the caller sets none. A
consumer with a bound of its own, such as the risk policy's timeout, keeps
its own bound. The value must be between 100 ms and 60 s. When it is unset,
the install's `integration.defaultTimeoutMs` applies (5 s).

It accepts Go duration syntax, which includes the protojson form: `"5s"`,
`"1.5s"`, `"500ms"`, `"1m"`. State keeps the spelling you wrote for as long
as kit stores the same length. An import has no spelling to keep and reads
the protojson form, `"1.5s"`, or `"60s"` rather than `"1m"`.

## Deleting, and what depends on what

kit refuses to delete an endpoint while anything names it. The error
(`FailedPrecondition`) names the holder and the field. Holders include an
issuer, client, audience, realm, provider or factor config, and an Access
type. For the same reason, kit refuses to delete a certificate while an
endpoint presents it as `tls.client_certificate`, and an issuer while an
endpoint's `kitToken` signs from it.

Terraform orders destroys by references, so the way you name the endpoint
matters:

- **Reference it**, even inside `jsonencode`, and the holder is destroyed
  first:

  ```terraform
  resource "authwise_audience" "api" {
    display_name = "https://api.example.com"
    config = jsonencode({
      flowIntegrationConfig = { endpointName = authwise_endpoint.flow.name }
    })
  }
  ```

  `tls.client_certificate = authwise_certificate.x.name` and
  `kitToken.issuer = authwise_issuer.x.name` order the same way.

- **A literal name string** such as `"tenants/t-01/endpoints/e-01"` gives
  Terraform no edge. Destroy may then remove the endpoint first and fail
  with the holder's name. Add `depends_on = [authwise_endpoint.flow]` to
  the holder, or better, replace the literal with a reference.

- **A holder outside this configuration**, such as another workspace or a
  console edit, blocks the destroy until it lets go. kit's `:referrers` RPC
  (`GET {name}:referrers`) lists every holder and the field it names the
  endpoint in.

## Checking an endpoint

kit's `:check` RPC dials the stored endpoint the way a consumer would. It
reports reachability, TLS verification, whether the credential resolved,
and, for gRPC, the services the endpoint offers. An unreachable endpoint
is a report with `reachable: false`, not an error. The check writes
nothing. It is an operational act rather than configuration, so the
provider does not run it. Call it directly: `POST {name}:check`
with an empty body.

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `address` (String)
- `auth` (String) `auth` as the protojson encoding of EndpointAuth.
- `display_name` (String)
- `endpoint_type` (String)
- `insecure` (Boolean)
- `labels` (Map of String)
- `tenant_id` (String) Parent identifier `tenant_id`; overrides the provider default. Changing it replaces the resource.
- `timeout` (String) `timeout` as a duration: `"5s"`, `"1.5s"`, `"500ms"`, `"1m30s"`.
- `tls` (Attributes) (see [below for nested schema](#nestedatt--tls))

### Read-Only

- `name` (String) Full resource name; serves as the Terraform ID.

<a id="nestedatt--tls"></a>
### Nested Schema for `tls`

Optional:

- `ca_pem` (String)
- `client_certificate` (String)
- `insecure_skip_verify` (Boolean)
- `server_name` (String)

## Import

```shell
# Import by full resource name.
terraform import authwise_endpoint.example tenants/t-01/endpoints/e-01
```
