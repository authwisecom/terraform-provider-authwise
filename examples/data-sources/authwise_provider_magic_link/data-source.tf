# A realm whose people sign themselves up with a magic link.
data "authwise_realm_config" "customers" {
  provisioning = {
    self_signup                = true
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
}

resource "authwise_provider" "magic_link" {
  realm_id      = authwise_realm.customers.realm_id
  display_name  = "Email me a link"
  provider_type = "magicLink"
  config        = data.authwise_provider_magic_link.email.any
}
