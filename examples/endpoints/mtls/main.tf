# A gRPC service on a private CA that authenticates kit by its client
# certificate (mutual TLS):
#
#   - a key pair kit mints and keeps — the private key never leaves kit;
#     give the certificate_pem output to the service to trust;
#   - the endpoint, trusting the service's CA and presenting that
#     certificate.

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
    # Added to the system roots, not in place of them. Public material,
    # stored and returned as is.
    ca_pem = file("${path.module}/payments-ca.pem")

    # A certificate of this tenant holding a private key; imported partner
    # certificates hold none and are refused. Naming it here orders the
    # endpoint after it.
    client_certificate = authwise_certificate.kit_client.name
  }
}

# Hand this to the payments service as the client certificate to trust.
output "kit_client_certificate_pem" {
  value = authwise_certificate.kit_client.certificate_pem
}
