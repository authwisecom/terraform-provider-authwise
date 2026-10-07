# Import by full resource name. An imported invite has no code or url:
# guard-control returns them only when the invite is created.
terraform import authwise_guard_invite.dana tenants/t-01/networks/n-01/invites/inv-01
