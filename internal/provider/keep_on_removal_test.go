package provider_test

// keepOnRemoval is every optional resource attribute that still keeps its
// last value when its line is removed from the configuration (the
// Optional+Computed shape with no default), each with why. Audit T1
// (kit docswip/audit_2026_11/awctl-terraform.md): a setting that outlives
// its line cannot be turned off by deleting it, so a new attribute must be
// clearable or be listed here with its reason.
// TestSchema_OptionalAttributesClearOrAreExempt enforces both directions.
// Keys are resource type and attribute path, nested children joined by ".".
// A field the server refuses empty is Required rather than listed here.
var keepOnRemoval = map[string]string{
	"authwise_application.kind":                                    reasonServerFills,
	"authwise_application.status":                                  reasonServerFills,
	"authwise_application.token_endpoint_auth_method":              reasonServerFills,
	"authwise_audience.status":                                     reasonServerFills,
	"authwise_certificate.status":                                  reasonServerFills,
	"authwise_domain.custom":                                       reasonReplaces,
	"authwise_domain.purpose":                                      reasonReplaces,
	"authwise_endpoint.endpoint_type":                              reasonServerFills,
	"authwise_factor.status":                                       reasonServerFills,
	"authwise_guard_enrollment_key.max_uses":                       reasonReplaces,
	"authwise_guard_network.cidr":                                  reasonReplaces,
	"authwise_guard_network.config":                                reasonGuardIgnoresConfig,
	"authwise_guard_network.relay_pool":                            reasonServerFills,
	"authwise_guard_network.slug":                                  reasonHeldTF80,
	"authwise_guard_node.address":                                  reasonReplaces,
	"authwise_guard_node.config":                                   reasonGuardIgnoresConfig,
	"authwise_guard_node.display_name":                             reasonWrittenElsewhere,
	"authwise_guard_node.endpoint":                                 reasonHeldTF80,
	"authwise_guard_node.public_key":                               reasonServerFills,
	"authwise_guard_node.slug":                                     reasonServerFills,
	"authwise_guard_pool_relay.config":                             reasonGuardIgnoresConfig,
	"authwise_guard_relay.config":                                  reasonGuardIgnoresConfig,
	"authwise_guard_relay_pool.config":                             reasonGuardIgnoresConfig,
	"authwise_guard_relay_pool.default":                            reasonLeavesNoDefault,
	"authwise_guard_relay_pool.select":                             reasonServerFills,
	"authwise_guard_resource.config":                               reasonGuardIgnoresConfig,
	"authwise_guard_resource.published_name":                       reasonServerFills,
	"authwise_guard_resource.translated":                           reasonReplaces,
	"authwise_guard_tenant_settings.config":                        reasonFailsClosed,
	"authwise_guard_tenant_settings.config.deprovisioning":         reasonFailsClosed,
	"authwise_guard_tenant_settings.config.deprovisioning.enabled": reasonFailsClosed,
	"authwise_guard_tenant_settings.config.users":                  reasonFailsClosed,
	"authwise_guard_tenant_settings.config.users.realms":           reasonFailsClosed,
	"authwise_guard_trusted_issuer_rule.ephemeral":                 reasonFailsClosed,
	"authwise_guard_trusted_issuer_rule.max_nodes":                 reasonFailsClosed,
	"authwise_issuer.domain_id":                                    reasonReplaces,
	"authwise_issuer.path":                                         reasonReplaces,
	"authwise_issuer.status":                                       reasonServerFills,
	"authwise_issuer_domain_binding.path":                          reasonReplaces,
	"authwise_provider.status":                                     reasonServerFills,
	"authwise_realm.status":                                        reasonServerFills,
	"authwise_scim_connection.identifier_attribute":                reasonServerFills,
	"authwise_scim_connection.on_delete":                           reasonServerFills,
	"authwise_scim_connection.status":                              reasonServerFills,
	"authwise_scim_token.expires_at":                               reasonReplaces,
	"authwise_service_account.roles":                               reasonRolesLeftAlone,
	"authwise_service_account.tenant_id":                           reasonReplaces,
	"authwise_tenant_config.tenant_id":                             reasonReplaces,
	"authwise_theme.layout":                                        reasonServerFills,
	"authwise_theme_variant.is_default":                            reasonLeavesNoDefault,
	"authwise_user.birthdate":                                      reasonWrittenElsewhere,
	"authwise_user.display_name":                                   reasonWrittenElsewhere,
	"authwise_user.external_id":                                    reasonWrittenElsewhere,
	"authwise_user.extra_fields":                                   reasonWrittenElsewhere,
	"authwise_user.family_name":                                    reasonWrittenElsewhere,
	"authwise_user.gender":                                         reasonWrittenElsewhere,
	"authwise_user.given_name":                                     reasonWrittenElsewhere,
	"authwise_user.locale":                                         reasonWrittenElsewhere,
	"authwise_user.metadata":                                       reasonWrittenElsewhere,
	"authwise_user.middle_name":                                    reasonWrittenElsewhere,
	"authwise_user.nickname":                                       reasonWrittenElsewhere,
	"authwise_user.picture":                                        reasonWrittenElsewhere,
	"authwise_user.preferred_username":                             reasonWrittenElsewhere,
	"authwise_user.profile":                                        reasonWrittenElsewhere,
	"authwise_user.status":                                         reasonServerFills,
	"authwise_user.website":                                        reasonWrittenElsewhere,
	"authwise_user.zoneinfo":                                       reasonWrittenElsewhere,
}

const (
	// reasonServerFills: kit or guard-control fills or defaults it when it
	// is left out, so a plan of null would read back a value: Terraform
	// would fail the apply as an inconsistent result.
	reasonServerFills = "the server fills or defaults it when left out"
	// reasonReplaces: it cannot change in place; removing the line keeps
	// the resource as it was created rather than replacing it.
	reasonReplaces = "immutable: changing it replaces the resource"
	// reasonFailsClosed: kept after its line is removed, it keeps a
	// narrowing or a limit in force; nothing is widened (ruled 2026-11-18).
	reasonFailsClosed = "kept, it still narrows or limits: fails closed"
	// reasonWrittenElsewhere: people, SCIM or node enrolment write it
	// outside Terraform (set B, ruled 2026-11-18).
	reasonWrittenElsewhere = "written outside Terraform by people, SCIM or enrolment"
	// reasonRolesLeftAlone: unset leaves the service account's roles alone,
	// by design, so roles set with awctl stay (ruled 2026-11-18).
	reasonRolesLeftAlone = "unset leaves the roles alone, by design"
	// reasonHeldTF80: held for tf#80's ruling.
	reasonHeldTF80 = "held for tf#80"
	// reasonGuardIgnoresConfig: guard-control's proto carries a config
	// Struct that no converter stores or returns, so a read is always null
	// whatever is written (guard-control pkg/lib/convert/convert.go).
	reasonGuardIgnoresConfig = "guard-control never stores or returns it"
	// reasonLeavesNoDefault: clearing the flag promotes nothing, so a removed
	// line would leave the issuer or the estate with no default at all. Set
	// false explicitly to do that on purpose.
	reasonLeavesNoDefault = "removing it would leave no default; set false explicitly"
)
