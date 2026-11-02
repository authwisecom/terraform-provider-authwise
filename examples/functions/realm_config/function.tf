resource "authwise_realm" "this" {
  display_name = "Customers"
  config = provider::authwise::realm_config({
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
  })
}
