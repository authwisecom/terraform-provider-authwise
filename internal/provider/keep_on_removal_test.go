package provider_test

// keepOnRemoval is every optional resource attribute that still keeps its
// last value when its line is removed from the configuration (the
// Optional+Computed shape with no default), each with why. Audit T1
// (kit docswip/audit_2026_11/awctl-terraform.md): a setting that outlives
// its line cannot be turned off by deleting it, so a new attribute must be
// clearable or be listed here with its reason.
// TestSchema_OptionalAttributesClearOrAreExempt enforces both directions.
// Keys are resource type and attribute path, nested children joined by ".".
var keepOnRemoval = map[string]string{
	"authwise_access_condition.display_name":                       reasonRequired,
	"authwise_access_condition.expression":                         reasonRequired,
	"authwise_access_permission.service":                           reasonRequired,
	"authwise_access_resource_type.materialize_lookup_resources":   reasonPending,
	"authwise_application.audience_id":                             reasonRequired,
	"authwise_application.config":                                  reasonPending,
	"authwise_application.display_name":                            reasonRequired,
	"authwise_application.kind":                                    reasonServerFills,
	"authwise_application.labels":                                  reasonPending,
	"authwise_application.status":                                  reasonServerFills,
	"authwise_application.token_endpoint_auth_method":              reasonServerFills,
	"authwise_asset.display_name":                                  reasonRequired,
	"authwise_asset.labels":                                        reasonPending,
	"authwise_asset.mime_type":                                     reasonRequired,
	"authwise_asset.path":                                          reasonRequired,
	"authwise_audience.config":                                     reasonPending,
	"authwise_audience.display_name":                               reasonRequired,
	"authwise_audience.labels":                                     reasonPending,
	"authwise_audience.status":                                     reasonServerFills,
	"authwise_certificate.labels":                                  reasonPending,
	"authwise_certificate.status":                                  reasonServerFills,
	"authwise_domain.custom":                                       reasonReplaces,
	"authwise_domain.labels":                                       reasonPending,
	"authwise_domain.purpose":                                      reasonReplaces,
	"authwise_endpoint.address":                                    reasonRequired,
	"authwise_endpoint.display_name":                               reasonRequired,
	"authwise_endpoint.endpoint_type":                              reasonServerFills,
	"authwise_endpoint.labels":                                     reasonPending,
	"authwise_endpoint.timeout":                                    reasonPending,
	"authwise_factor.config":                                       reasonPending,
	"authwise_factor.labels":                                       reasonPending,
	"authwise_factor.status":                                       reasonServerFills,
	"authwise_guard_enrollment_key.max_uses":                       reasonReplaces,
	"authwise_guard_network.cidr":                                  reasonReplaces,
	"authwise_guard_network.config":                                reasonPending,
	"authwise_guard_network.labels":                                reasonPending,
	"authwise_guard_network.relay_pool":                            reasonServerFills,
	"authwise_guard_network.slug":                                  reasonHeldTF80,
	"authwise_guard_node.address":                                  reasonReplaces,
	"authwise_guard_node.config":                                   reasonPending,
	"authwise_guard_node.display_name":                             reasonWrittenElsewhere,
	"authwise_guard_node.endpoint":                                 reasonHeldTF80,
	"authwise_guard_node.labels":                                   reasonPending,
	"authwise_guard_node.public_key":                               reasonServerFills,
	"authwise_guard_node.slug":                                     reasonServerFills,
	"authwise_guard_pool_relay.config":                             reasonPending,
	"authwise_guard_pool_relay.labels":                             reasonPending,
	"authwise_guard_pool_relay.priority":                           reasonPending,
	"authwise_guard_relay.config":                                  reasonPending,
	"authwise_guard_relay.labels":                                  reasonPending,
	"authwise_guard_relay.priority":                                reasonPending,
	"authwise_guard_relay_pool.config":                             reasonPending,
	"authwise_guard_relay_pool.default":                            reasonPending,
	"authwise_guard_relay_pool.labels":                             reasonPending,
	"authwise_guard_relay_pool.select":                             reasonServerFills,
	"authwise_guard_relay_pool.slug":                               reasonRequired,
	"authwise_guard_resource.config":                               reasonPending,
	"authwise_guard_resource.labels":                               reasonPending,
	"authwise_guard_resource.published_name":                       reasonServerFills,
	"authwise_guard_resource.translated":                           reasonReplaces,
	"authwise_guard_tenant_settings.config":                        reasonFailsClosed,
	"authwise_guard_tenant_settings.config.deprovisioning":         reasonFailsClosed,
	"authwise_guard_tenant_settings.config.deprovisioning.enabled": reasonFailsClosed,
	"authwise_guard_tenant_settings.config.users":                  reasonFailsClosed,
	"authwise_guard_tenant_settings.config.users.realms":           reasonFailsClosed,
	"authwise_guard_tenant_settings.labels":                        reasonPending,
	"authwise_guard_trusted_issuer_rule.ephemeral":                 reasonFailsClosed,
	"authwise_guard_trusted_issuer_rule.max_nodes":                 reasonFailsClosed,
	"authwise_issuer.config":                                       reasonPending,
	"authwise_issuer.domain_id":                                    reasonReplaces,
	"authwise_issuer.labels":                                       reasonPending,
	"authwise_issuer.path":                                         reasonReplaces,
	"authwise_issuer.status":                                       reasonServerFills,
	"authwise_issuer_domain_binding.path":                          reasonReplaces,
	"authwise_provider.config":                                     reasonPending,
	"authwise_provider.display_name":                               reasonRequired,
	"authwise_provider.labels":                                     reasonPending,
	"authwise_provider.provider_type":                              reasonRequired,
	"authwise_provider.status":                                     reasonServerFills,
	"authwise_realm.config":                                        reasonPending,
	"authwise_realm.labels":                                        reasonPending,
	"authwise_realm.status":                                        reasonServerFills,
	"authwise_scim_connection.identifier_attribute":                reasonServerFills,
	"authwise_scim_connection.labels":                              reasonPending,
	"authwise_scim_connection.on_delete":                           reasonServerFills,
	"authwise_scim_connection.status":                              reasonServerFills,
	"authwise_scim_token.expires_at":                               reasonReplaces,
	"authwise_scope.kind":                                          reasonRequired,
	"authwise_secret.description":                                  reasonPending,
	"authwise_secret.display_name":                                 reasonPending,
	"authwise_secret.labels":                                       reasonPending,
	"authwise_service_account.roles":                               reasonRolesLeftAlone,
	"authwise_service_account.tenant_id":                           reasonReplaces,
	"authwise_tenant_config.tenant_id":                             reasonReplaces,
	"authwise_theme.content":                                       reasonPending,
	"authwise_theme.display_name":                                  reasonRequired,
	"authwise_theme.labels":                                        reasonPending,
	"authwise_theme.layout":                                        reasonServerFills,
	"authwise_theme.placeholder_content":                           reasonPending,
	"authwise_theme.placeholder_stylesheet_attributes":             reasonPending,
	"authwise_theme.stylesheet_attributes":                         reasonPending,
	"authwise_theme_variant.content":                               reasonPending,
	"authwise_theme_variant.display_name":                          reasonRequired,
	"authwise_theme_variant.is_default":                            reasonPending,
	"authwise_theme_variant.labels":                                reasonPending,
	"authwise_theme_variant.stylesheet_attributes":                 reasonPending,
	"authwise_theme_variant.theme_id":                              reasonRequired,
	"authwise_user.birthdate":                                      reasonWrittenElsewhere,
	"authwise_user.display_name":                                   reasonWrittenElsewhere,
	"authwise_user.external_id":                                    reasonWrittenElsewhere,
	"authwise_user.extra_fields":                                   reasonWrittenElsewhere,
	"authwise_user.family_name":                                    reasonWrittenElsewhere,
	"authwise_user.gender":                                         reasonWrittenElsewhere,
	"authwise_user.given_name":                                     reasonWrittenElsewhere,
	"authwise_user.labels":                                         reasonPending,
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
	// reasonRequired: kit or guard-control refuses it empty, so removing
	// the line could never clear it. The remaining T1 batch makes these
	// Required, as grant_types now is.
	reasonRequired = "the server refuses it empty; remaining T1 batch: Required"
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
	// reasonPending: no security effect; the remaining T1 batch decides it
	// (labels, whole-attribute config JSON after a per-type check, and the
	// rest).
	reasonPending = "no security effect; remaining T1 batch"
)
