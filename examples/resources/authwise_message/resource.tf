# The invitation's own words: the named parts of the built-in message.
# A part left out keeps kit's wording; kit's layout and the tenant's brand
# render them. A part that differs by case is set per case, such as
# "body.join".
resource "authwise_message" "invitation" {
  key    = "tenant-invitation"
  locale = "en"

  texts = {
    subject  = "Welcome to {{ .Brand.DisplayName }}"
    greeting = "Hello {{ .Recipient.DisplayName }},"
    button   = "Accept the invitation"
  }
}

# The same message in French, written whole.
resource "authwise_message" "invitation_fr" {
  key    = "tenant-invitation"
  locale = "fr"

  subject = "Bienvenue chez {{ .Brand.DisplayName }}"
  html    = file("${path.module}/mail/invitation.fr.html")
  text    = file("${path.module}/mail/invitation.fr.txt")
}
