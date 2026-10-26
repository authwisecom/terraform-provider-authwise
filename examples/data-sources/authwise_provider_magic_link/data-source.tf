# A realm whose people sign themselves up with a magic link, or are
# invited. Invitations are off on a realm created through the API until
# its config turns them on, and from kit v1.59.1 an invite is refused.
data "authwise_realm_config" "customers" {
  provisioning = {
    self_signup                = true
    invitations                = true
    require_email_verification = true
  }
}

resource "authwise_realm" "customers" {
  display_name = "Customers"
  config       = data.authwise_realm_config.customers.json
}

data "authwise_provider_magic_link" "email" {
  # Unset, these are kit's defaults: the link signs in only the browser
  # that asked for it, and an address with no account reads "sent" like
  # any other. Each alternative trades something away.
  link_scope      = "ANY_BROWSER" # whoever reads the mailbox can sign in anywhere: add a step-up rule
  unknown_address = "TELL"        # says "no account", revealing which addresses have one

  # TELL answers at the request throttle's rate, so keep the per-address
  # and per-IP caps low. Unset limits take kit's defaults (3, 10, 500).
  request_limits = {
    per_identifier_per_hour = 2 # 1 to 20
    per_ip_per_hour         = 5 # 1 to 100
  }
}

resource "authwise_provider" "magic_link" {
  realm_id      = authwise_realm.customers.realm_id
  display_name  = "Email me a link"
  provider_type = "magicLink"
  config        = data.authwise_provider_magic_link.email.any
}
