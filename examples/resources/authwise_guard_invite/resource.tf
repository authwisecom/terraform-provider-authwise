# An invite for one person, granting the office LAN to the node they join
# with. The code and url are shown once and kept in state; send the url.
# A spent or expired invite stays in state: replace it to issue another,
# with `terraform apply -replace=authwise_guard_invite.dana`.
resource "authwise_guard_invite" "dana" {
  tenant_id    = "gt-01" # the Guard tenant, from awtenant
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "for Dana"
  grants       = [authwise_guard_resource.office_lan.name]
  max_uses     = 1
}

output "dana_invite_url" {
  value     = authwise_guard_invite.dana.url
  sensitive = true
}
