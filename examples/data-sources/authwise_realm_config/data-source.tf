data "authwise_realm_config" "this" {
  default_locale = "en"

  provisioning = {
    self_signup                = true
    require_email_verification = true
  }

  recovery = {
    self_service_reset = true
    reset_ttl          = "30m"
  }

  bot_protection = {
    mode       = "ALWAYS"
    provider   = "TURNSTILE"
    site_key   = "0x4AAAAAAA"
    secret_ref = { name = authwise_secret.turnstile.name }
  }

  # Send the welcome message once to a person who signs up themselves.
  signup = {
    welcome_message = true
  }
}

resource "authwise_realm" "this" {
  display_name = "Customers"
  config       = data.authwise_realm_config.this.json
}
