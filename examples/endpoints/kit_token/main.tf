# A gRPC service that verifies a token kit signs, so no secret is stored
# anywhere:
#
#   - the issuer kit signs with — the service verifies the token against
#     this issuer's JWKS;
#   - the endpoint, whose kitToken scheme sends
#     "authorization: Bearer <token>" with aud set to the audience below,
#     sub "authwise-internal" and a five-minute lifetime.

resource "authwise_issuer" "internal" {
  domain_name = "auth.example.com"
  path        = "/internal"
}

resource "authwise_endpoint" "ledger" {
  display_name  = "Ledger"
  endpoint_type = "GRPC"

  # host:port, or a dns:///host:port target.
  address = "ledger.example.com:443"

  timeout = "1500ms"

  # The certificate the ledger presents is for its internal name, not the
  # public one in the address.
  tls = {
    server_name = "ledger.internal"
  }

  # protojson keys are lowerCamel: kitToken, not kit_token. Naming the
  # issuer here orders the endpoint after it.
  auth = jsonencode({
    kitToken = {
      issuer   = authwise_issuer.internal.name
      audience = "https://ledger.example.com"
    }
  })
}
