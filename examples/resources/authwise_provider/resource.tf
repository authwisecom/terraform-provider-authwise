# Realm-scoped: the provider people sign in with, its config built by a
# config data source and its client secret held by reference.
resource "authwise_secret" "google" {
  display_name = "Google client secret"
  payload_wo   = var.google_client_secret
}

data "authwise_provider_google" "this" {
  client_id         = "123456789012-abc.apps.googleusercontent.com"
  client_secret_ref = { name = authwise_secret.google.name }
}

resource "authwise_provider" "google" {
  realm_id      = element(split("/", authwise_realm.employees.name), 3)
  display_name  = "Sign in with Google"
  provider_type = "google"
  config        = data.authwise_provider_google.this.any
}
