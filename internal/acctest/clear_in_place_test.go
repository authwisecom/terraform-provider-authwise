// Clearable attributes (tf#79): optional inputs kit or guard-control store
// exactly as sent and never fill themselves. Removed from the configuration,
// each plans null, is cleared by an in-place update, and the next plan is
// empty. Before tf#79 they were Optional+Computed, so the plan kept the state
// value and nothing could clear them short of a replacement.
package acctest_test

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// clearCase is one resource: what it needs around it, the arguments it always
// takes, and the clearable ones with a value to set and then remove.
type clearCase struct {
	// typ is the resource type without the provider prefix.
	typ string
	// prereq is the HCL the resource depends on (after the provider block).
	prereq string
	// base is the resource's other arguments, one per line.
	base string
	// clear maps each clearable attribute to the HCL value it is set to.
	// A bool is set true and an integer to a number: removed, they plan
	// and read false and 0, not null.
	clear map[string]string
}

func (c clearCase) addr() string { return "authwise_" + c.typ + ".x" }

func (c clearCase) config(h *harness, withClearable bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\nresource \"authwise_%s\" \"x\" {\n%s\n", h.providerConfig(), c.prereq, c.typ, c.base)
	if withClearable {
		for _, attr := range c.attrs() {
			fmt.Fprintf(&b, "  %s = %s\n", attr, c.clear[attr])
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func (c clearCase) attrs() []string {
	out := make([]string, 0, len(c.clear))
	for attr := range c.clear {
		out = append(out, attr)
	}
	sort.Strings(out)
	return out
}

// removed is what a clearable attribute plans and reads once its line is
// gone: false for a bool and 0 for an integer, which proto3 cannot leave
// unset, null otherwise.
func (c clearCase) removed(attr string) knownvalue.Check {
	if c.clear[attr] == "true" {
		return knownvalue.Bool(false)
	}
	if _, err := strconv.ParseInt(c.clear[attr], 10, 64); err == nil {
		return knownvalue.Int64Exact(0)
	}
	return knownvalue.Null()
}

// clearInPlace sets every clearable attribute, removes them all, and expects
// an in-place update to null (false for a bool) that the server applies,
// then an empty plan, then setting them again in place.
func clearInPlace(t *testing.T, c clearCase) {

	h := newHarness(t)
	addr := c.addr()
	full, bare := c.config(h, true), c.config(h, false)

	attrs := c.attrs()
	planned := make([]plancheck.PlanCheck, 0, len(attrs))
	set := make([]statecheck.StateCheck, 0, len(attrs))
	gone := make([]statecheck.StateCheck, 0, len(attrs))
	for _, attr := range attrs {
		path := tfjsonpath.New(attr)
		planned = append(planned, plancheck.ExpectKnownValue(addr, path, c.removed(attr)))
		var on knownvalue.Check = knownvalue.NotNull()
		if c.clear[attr] == "true" {
			on = knownvalue.Bool(true)
		}
		set = append(set, statecheck.ExpectKnownValue(addr, path, on))
		gone = append(gone, statecheck.ExpectKnownValue(addr, path, c.removed(attr)))
	}

	var id string
	keepID := func(s *terraform.State) error {
		id = s.RootModule().Resources[addr].Primary.ID
		return nil
	}
	sameID := func(s *terraform.State) error {
		if got := s.RootModule().Resources[addr].Primary.ID; got != id {
			return fmt.Errorf("%s was replaced (%s, was %s); it should be updated in place", addr, got, id)
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:            full,
				ConfigStateChecks: set,
				Check:             keepID,
			},
			{
				// Removed: an in-place update to null or false, which the
				// read confirms, so the server cleared them.
				Config: bare,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: append([]plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}, planned...),
				},
				ConfigStateChecks: gone,
				Check:             sameID,
			},
			{
				Config:           bare,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// Set again, in place.
				Config: full,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: slices.Clone(set),
				Check:             sameID,
			},
		},
	})
}

const clearGuardNetwork = guardTenantHCL + `
resource "authwise_guard_network" "office" {
  tenant_id    = local.guard_tenant_id
  display_name = "Office"
}
`

const clearPEM = `<<-EOT
-----BEGIN CERTIFICATE-----
MIIBszCCAVmgAwIBAgIUCAm8fQe8b2ZbXH0oN6Hc0qY8jXcwCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----
EOT`

var clearCases = map[string]clearCase{
	"access_condition": {
		typ:   "access_condition",
		base:  `  display_name = "Office network"` + "\n" + `  expression   = "request.ip == \"192.0.2.1\""`,
		clear: map[string]string{"description": `"Requests from the office"`},
	},
	"access_permission": {
		typ:   "access_permission",
		base:  `  access_permission_id = "tf79.things.get"` + "\n" + `  service              = "tf79"`,
		clear: map[string]string{"description": `"Read things"`, "resource_type": `"tenant"`},
	},
	"access_resource_type": {
		typ:  "access_resource_type",
		base: `  access_resource_type_id = "tf79.thing"`,
		clear: map[string]string{
			"materialize_lookup_resources": "true",
			"description":                  `"A thing"`,
			"endpoint_name":                `"things"`,
			"expansion_mode":               `"PUSH"`,
			"parent_type":                  `"tenant"`,
		},
	},
	"access_role": {
		typ:   "access_role",
		base:  `  access_role_id = "tf79.viewer"`,
		clear: map[string]string{"description": `"Reads things"`, "display_name": `"Thing viewer"`},
	},
	"application": {
		typ: "application",
		prereq: `
data "authwise_interactive_client_config" "c" {
  allowed_redirect_uris = ["https://app.example.com/cb"]
}
`,
		base: `  display_name = "App"` + "\n" + `  audience_id  = "a-1"` + "\n" + `  grant_types  = ["authorization_code", "refresh_token"]`,
		clear: map[string]string{
			"labels":           `{ team = "identity" }`,
			"config":           "data.authwise_interactive_client_config.c.any",
			"application_url":  `"https://app.example.com"`,
			"login_url":        `"https://app.example.com/login"`,
			"theme_variant_id": `"ap-01"`,
			"expires_at":       `"2099-01-01T00:00:00Z"`,
			// Allowed redirect targets after logout (audit T1).
			"post_logout_redirect_uris": `["https://app.example.com/bye"]`,
		},
	},
	"application_secret": {
		typ: "application_secret",
		prereq: `
resource "authwise_application" "app" {
  audience_id = "a-1"
  display_name = "CI"
  grant_types  = ["client_credentials"]
}
`,
		base:  `  application_id = authwise_application.app.application_id`,
		clear: map[string]string{"expires_at": `"2099-01-01T00:00:00Z"`},
	},
	"audience": {
		typ:  "audience",
		base: `  display_name = "API"`,
		clear: map[string]string{
			"config":           `jsonencode({ accessTokenClaimScopes = ["email"] })`,
			"theme_variant_id": `"ap-01"`,
			"labels":           `{ team = "identity" }`,
		},
	},
	"realm": {
		typ: "realm",
		clear: map[string]string{
			"config":       `jsonencode({ defaultLocale = "de" })`,
			"display_name": `"Employees"`,
			"labels":       `{ team = "identity" }`,
		},
	},
	"scim_connection": {
		typ:    "scim_connection",
		prereq: realmForFactors,
		base:   `  realm_id     = local.realm_id` + "\n" + `  display_name = "Entra ID"` + "\n" + `  issuer_id    = "i-1"`,
		clear: map[string]string{
			"provider_id": `"p-1"`,
			"labels":      `{ team = "identity" }`,
		},
	},
	"theme": {
		typ:  "theme",
		base: `  display_name = "Northwind"`,
		clear: map[string]string{
			"content":                           `jsonencode({ headline = "Hello" })`,
			"placeholder_content":               `jsonencode({ headline = "Hi" })`,
			"placeholder_stylesheet_attributes": `jsonencode({ accent = "navy" })`,
			"stylesheet_attributes":             `jsonencode({ accent = "navy" })`,
			"labels":                            `{ team = "identity" }`,
			"content_schema":                    `"{}"`,
			"stylesheet":                        `"body { color: navy; }"`,
			"stylesheet_attributes_schema":      `"{}"`,
		},
	},
	// The trust switches and the map c reads (audit T1).
	"provider": {
		typ: "provider",
		prereq: realmForFactors + `
data "authwise_provider_google" "this" {
  client_id = "123.apps.googleusercontent.com"
}
`,
		base: `  realm_id      = local.realm_id` + "\n" + `  display_name  = "Google"` + "\n" + `  provider_type = "google"`,
		clear: map[string]string{
			"config":                        "data.authwise_provider_google.this.any",
			"labels":                        `{ team = "identity" }`,
			"link_by_verified_email":        "true",
			"trust_upstream_email_verified": "true",
			"trust_upstream_amr":            "true",
			"upstream_acr_map":              `{ "urn:partner:mfa" = "mfa" }`,
		},
	},
	// Plaintext, and the credential kit presents (audit T1).
	"endpoint": {
		typ: "endpoint",
		prereq: `
resource "authwise_domain" "internal" {
  hostname = "auth.example.com"
}

resource "authwise_issuer" "internal" {
  domain_id = authwise_domain.internal.domain_id
}
`,
		base: `  display_name  = "Ledger"` + "\n" + `  endpoint_type = "GRPC"` + "\n" + `  address       = "ledger.example.com:443"`,
		clear: map[string]string{
			"labels":   `{ team = "identity" }`,
			"insecure": "true",
			"auth":     `jsonencode({ kitToken = { issuer = authwise_issuer.internal.name, audience = "https://ledger.example.com" } })`,
		},
	},
	// How kit verifies the endpoint, as a whole block (audit T1).
	"endpoint_tls": {
		typ:  "endpoint",
		base: `  display_name  = "Ledger"` + "\n" + `  endpoint_type = "GRPC"` + "\n" + `  address       = "ledger.example.com:443"`,
		clear: map[string]string{
			"tls": `{ ca_pem = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUCAm8fQe8b2ZbXH0oN6Hc0qY8jXcwCgYIKoZIzj0EAwIw\n-----END CERTIFICATE-----\n", server_name = "ledger.internal", insecure_skip_verify = true }`,
		},
	},
	// Granted in every token unrequested (audit T1).
	"scope": {
		typ:   "scope",
		base:  `  scope_id = "tf79.read"` + "\n" + `  kind     = "access"`,
		clear: map[string]string{"auto": "true"},
	},
	// Audit T1's remaining batch: labels, the configs and the rest.
	"issuer": {
		typ: "issuer",
		clear: map[string]string{
			"labels": `{ team = "identity" }`,
			"config": `jsonencode({ accessTokenExpireSeconds = 900 })`,
		},
	},
	"factor": {
		typ: "factor",
		prereq: realmForFactors + `
data "authwise_factor_totp" "totp" {
  digits = 8
}
`,
		base: `  realm_id     = local.realm_id` + "\n" + `  display_name = "Authenticator app"` + "\n" + `  factor_type  = "totp"`,
		clear: map[string]string{
			"labels": `{ team = "identity" }`,
			"config": "data.authwise_factor_totp.totp.any",
		},
	},
	"user": {
		typ:    "user",
		prereq: realmForFactors,
		base:   `  realm_id = local.realm_id`,
		clear:  map[string]string{"labels": `{ team = "identity" }`},
	},
	"asset": {
		typ:   "asset",
		base:  `  display_name = "Logo"` + "\n" + `  path         = "/logo.svg"` + "\n" + `  mime_type    = "image/svg+xml"`,
		clear: map[string]string{"labels": `{ team = "brand" }`},
	},
	"certificate": {
		typ:   "certificate",
		base:  `  display_name        = "Signing"` + "\n" + `  use                 = "CERTIFICATE_USE_SIGNING"` + "\n" + `  subject_common_name = "sp.example.com"`,
		clear: map[string]string{"labels": `{ team = "identity" }`},
	},
	"domain": {
		typ:   "domain",
		base:  `  hostname = "login.example.com"`,
		clear: map[string]string{"labels": `{ team = "identity" }`},
	},
	"secret": {
		typ:  "secret",
		base: `  display_name = "Token"` + "\n" + `  payload_wo   = "token-material"`,
		clear: map[string]string{
			"labels":      `{ team = "identity" }`,
			"description": `"The ledger's token"`,
		},
	},
	"theme_variant": {
		typ:  "theme_variant",
		base: `  display_name = "Brand"` + "\n" + `  theme_id     = "th-1"`,
		clear: map[string]string{
			"labels":                `{ team = "brand" }`,
			"content":               `jsonencode({ headline = "Welcome back" })`,
			"stylesheet_attributes": `jsonencode({ accent = "navy" })`,
		},
	},
	"endpoint_timeout": {
		typ:   "endpoint",
		base:  `  display_name = "Risk"` + "\n" + `  address      = "https://risk.example.com/v1"`,
		clear: map[string]string{"timeout": `"5s"`},
	},
	"guard_network": {
		typ:    "guard_network",
		prereq: guardTenantHCL,
		base:   `  tenant_id = local.guard_tenant_id`,
		clear: map[string]string{
			"display_name": `"Office"`,
			"labels":       `{ team = "identity" }`,
		},
	},
	"guard_node": {
		typ:    "guard_node",
		prereq: clearGuardNetwork,
		base: `  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "db"
  public_key   = "ZGItcHVibGljLWtleQ=="`,
		clear: map[string]string{
			"region": `"us-west1"`,
			"labels": `{ team = "identity" }`,
		},
	},
	"guard_node_group": {
		typ:    "guard_node_group",
		prereq: clearGuardNetwork,
		base: `  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "pods"`,
		clear: map[string]string{
			"description": `"Pods of the cluster"`,
		},
	},
	"guard_relay": {
		typ:    "guard_relay",
		prereq: clearGuardNetwork,
		base: `  tenant_id  = local.guard_tenant_id
  network_id = authwise_guard_network.office.guard_network_id
  url        = "wss://relay.example.com:8443/v1/transport"`,
		clear: map[string]string{
			"priority":     "2",
			"display_name": `"us-west"`, "region": `"us-west1"`, "ca_cert_pem": clearPEM,
			"labels": `{ team = "identity" }`,
		},
	},
	"guard_relay_pool": {
		typ:  "guard_relay_pool",
		base: `  slug = "us-central"`,
		clear: map[string]string{
			"display_name": `"US central"`, "region": `"us-central1"`,
			"labels": `{ team = "identity" }`,
		},
	},
	"guard_pool_relay": {
		typ: "guard_pool_relay",
		prereq: `
resource "authwise_guard_relay_pool" "central" {
  slug = "us-central"
}
`,
		base: `  pool = authwise_guard_relay_pool.central.name` + "\n" + `  url  = "wss://c1.relay.example.com/v1/transport"`,
		clear: map[string]string{
			"priority":     "2",
			"display_name": `"c1"`, "region": `"us-central1"`, "ca_cert_pem": clearPEM,
			"labels": `{ team = "identity" }`,
		},
	},
	"guard_resource": {
		typ:    "guard_resource",
		prereq: clearGuardNetwork,
		base: `  tenant_id  = local.guard_tenant_id
  network_id = authwise_guard_network.office.guard_network_id
  kind       = "host"
  address    = "10.0.0.5"`,
		clear: map[string]string{
			"display_name": `"wiki"`,
			"labels":       `{ team = "identity" }`,
		},
	},
	"guard_trusted_issuer_rule": {
		typ: "guard_trusted_issuer_rule",
		prereq: clearGuardNetwork + fmt.Sprintf(`
resource "authwise_guard_trusted_issuer" "gke" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "gfk-nonprod-uc1"
  issuer       = %q
  jwks         = %q
}

resource "authwise_guard_node_group" "pods" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "pods"
}
`, gkIssuer, gkJWKS),
		base: `  tenant_id         = local.guard_tenant_id
  network_id        = authwise_guard_network.office.guard_network_id
  trusted_issuer_id = authwise_guard_trusted_issuer.gke.guard_trusted_issuer_id
  subject           = "system:serviceaccount:argocd:argocd-application-controller"`,
		clear: map[string]string{
			"description":      `"Argo CD's controller"`,
			"node_group":       "authwise_guard_node_group.pods.name",
			"node_name_prefix": `"argocd"`,
		},
	},
}

func TestAccClearInPlace(t *testing.T) {
	for _, name := range keys(clearCases) {
		t.Run(name, func(t *testing.T) { clearInPlace(t, clearCases[name]) })
	}
}
