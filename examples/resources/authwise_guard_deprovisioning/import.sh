# Import by the Guard tenant's id. The secret cannot be recovered: nothing
# returns it. Bump client_secret_wo_version to send it again after importing.
terraform import authwise_guard_deprovisioning.this gt-01
