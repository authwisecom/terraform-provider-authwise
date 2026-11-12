# Import by key/channel/locale, with the tenant first when it is not the
# provider's default. The message keeps its wording until the next apply.
terraform import authwise_message.invitation tenant-invitation/email/en
terraform import authwise_message.invitation t-01/tenant-invitation/email/en
