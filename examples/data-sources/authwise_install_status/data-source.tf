# The install's license and registration, read with an operator's
# credential.
data "authwise_install_status" "this" {}

# Fail the plan when the license is not valid.
check "license" {
  assert {
    condition     = data.authwise_install_status.this.license.state == "valid"
    error_message = "The install's license is ${data.authwise_install_status.this.license.state}."
  }
}

# An install that cannot reach the portal uploads this to register.
output "registration_payload" {
  value = data.authwise_install_status.this.registration.payload
}
