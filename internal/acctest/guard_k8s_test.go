// Guard for Kubernetes (train 2026.11.GFK.1, GK5): node groups, their
// members and grants, a cluster trust and its rule, an enrollment key that
// fills a group, and a translated application served by a group. Against
// the fake's reading of apis v0.73.0: guard-control's servers were not
// built yet.
package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	gkPodsAddr    = "authwise_guard_node_group.pods"
	gkAdminsAddr  = "authwise_guard_node_group.admins"
	gkTrustAddr   = "authwise_guard_cluster_trust.gke"
	gkRuleAddr    = "authwise_guard_cluster_trust_rule.argocd"
	gkKeyAddr     = "authwise_guard_enrollment_key.connector"
	gkPromAddr    = "authwise_guard_resource.prometheus"
	gkMembersAddr = "authwise_guard_node_group_members.admins"
	gkIssuer      = "https://container.googleapis.com/v1/projects/p/locations/us-central1-a/clusters/gfk"
	gkJWKS        = `{"keys":[{"kty":"RSA","kid":"k1","n":"AQAB","e":"AQAB"}]}`
)

// guardK8sConfig is a network with two groups, a cluster trust with its
// keys given by keys (an HCL line), a rule and a key filling "pods", a
// translated application served by "pods", and an admin node in "admins",
// which may reach "pods".
func (h *harness) guardK8sConfig(keys, ruleMax string) string {
	return h.providerConfig() + fmt.Sprintf(`
`+guardTenantHCL+`
resource "authwise_guard_network" "office" {
  tenant_id    = local.guard_tenant_id
  display_name = "Office"
  cidr         = "100.96.0.0/16"
}

resource "authwise_guard_node_group" "pods" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "gfk pods"
  description  = "Pods of the gfk cluster"
}

resource "authwise_guard_node_group" "admins" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "admins"
}

resource "authwise_guard_node" "admin" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "admin"
  public_key   = "YWRtaW4tcHVibGljLWtleQ=="
}

resource "authwise_guard_node_group_members" "admins" {
  node_group = authwise_guard_node_group.admins.name
  nodes      = [authwise_guard_node.admin.name]
}

resource "authwise_guard_node_group_grants" "admins" {
  node_group = authwise_guard_node_group.admins.name
  grants     = [authwise_guard_node_group.pods.name]
}

resource "authwise_guard_node_group_grants" "pods" {
  node_group = authwise_guard_node_group.pods.name
  grants     = [authwise_guard_resource.prometheus.name]
}

resource "authwise_guard_cluster_trust" "gke" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "gfk-nonprod-uc1"
  issuer       = %q
  %s
}

resource "authwise_guard_cluster_trust_rule" "argocd" {
  tenant_id        = local.guard_tenant_id
  network_id       = authwise_guard_network.office.guard_network_id
  cluster_trust_id = authwise_guard_cluster_trust.gke.guard_cluster_trust_id
  namespace        = "argocd"
  service_account  = "argocd-application-controller"
  node_name_prefix = "argocd"
  ephemeral        = true
  node_group       = authwise_guard_node_group.pods.name
  %s
}

resource "authwise_guard_enrollment_key" "connector" {
  tenant_id        = local.guard_tenant_id
  network_id       = authwise_guard_network.office.guard_network_id
  description      = "gfk connector"
  expiry_days      = 7
  max_uses         = 4
  ephemeral        = true
  node_name_prefix = "conn"
  node_group       = authwise_guard_node_group.pods.name
}

resource "authwise_guard_resource" "prometheus" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "prometheus"
  kind         = "application"
  address      = "prometheus.monitoring.svc.cluster.local"
  translated   = true
}

resource "authwise_guard_resource_node_groups" "prometheus" {
  resource    = authwise_guard_resource.prometheus.name
  node_groups = [authwise_guard_node_group.pods.name]
}
`, gkIssuer, keys, ruleMax)
}

func TestAccGuardK8s_GroupsTrustKey(t *testing.T) {

	h := newHarness(t)

	var keyName, secret, trustName, trustID string
	var joined []string
	discover := h.guardK8sConfig("discover = true", "")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: discover,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(gkPodsAddr, "guard_node_group_id", regexp.MustCompile(`^ng-`)),
					resource.TestCheckResourceAttr(gkPodsAddr, "member_count", "0"),
					resource.TestMatchResourceAttr(gkTrustAddr, "guard_cluster_trust_id", regexp.MustCompile(`^ct-`)),
					resource.TestMatchResourceAttr(gkTrustAddr, "audience", regexp.MustCompile(`^guard:tenants/t-1/networks/n-`)),
					resource.TestCheckResourceAttr(gkTrustAddr, "key_ids.0", "kid-discovered"),
					resource.TestCheckNoResourceAttr(gkTrustAddr, "keys_error"),
					resource.TestCheckResourceAttr(gkRuleAddr, "max_nodes", "100"),
					resource.TestCheckResourceAttr(gkRuleAddr, "node_count", "0"),
					resource.TestMatchResourceAttr(gkRuleAddr, "name", regexp.MustCompile(`/clusterTrusts/ct-[0-9]+/rules/ctr-`)),
					resource.TestMatchResourceAttr(gkKeyAddr, "secret", regexp.MustCompile(`^gk_`)),
					resource.TestCheckResourceAttr(gkKeyAddr, "state", "ACTIVE"),
					resource.TestCheckResourceAttr(gkKeyAddr, "uses", "0"),
					resource.TestCheckResourceAttrSet(gkKeyAddr, "expires_at"),
					resource.TestCheckResourceAttr(gkPromAddr, "translated", "true"),
					resource.TestMatchResourceAttr(gkPromAddr, "virtual_address", regexp.MustCompile(`^100\.96\.`)),
					resource.TestCheckResourceAttr(gkPromAddr, "published_name", "prometheus.monitoring.office.acme.guard.example"),
					resource.TestCheckResourceAttrWith(gkKeyAddr, "name", func(v string) error { keyName = v; return nil }),
					resource.TestCheckResourceAttrWith(gkKeyAddr, "secret", func(v string) error { secret = v; return nil }),
					resource.TestCheckResourceAttrWith(gkTrustAddr, "name", func(v string) error { trustName = v; return nil }),
					resource.TestCheckResourceAttrWith(gkTrustAddr, "guard_cluster_trust_id", func(v string) error { trustID = v; return nil }),
				),
			},
			{
				// A connector replica joins with the key: it is in the
				// group at once, the key counts the use, and Terraform has
				// nothing to do.
				// Computed values other resources set (members, serving
				// groups) read back on this refresh.
				PreConfig: func() { joined = append(joined, h.guard.joinWithKey(keyName, "conn-0")) },
				Config:    discover,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gkAdminsAddr, "member_count", "1"),
					resource.TestCheckResourceAttrPair(gkPromAddr, "serving_node_groups.0", gkPodsAddr, "name"),
					resource.TestCheckResourceAttr(gkKeyAddr, "uses", "1"),
					resource.TestCheckResourceAttr(gkPodsAddr, "member_count", "1"),
					resource.TestCheckResourceAttrWith(gkKeyAddr, "secret", equalTo(&secret)),
				),
			},
			{
				// The rule's cap changes in place, for joins from now on.
				Config: h.guardK8sConfig("discover = true", "max_nodes = 20"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gkRuleAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr(gkRuleAddr, "max_nodes", "20"),
			},
			{
				// A private cluster's keys: the trust moves to an upload in
				// place, and keeps its id.
				Config: h.guardK8sConfig("jwks = "+fmt.Sprintf("%q", gkJWKS), "max_nodes = 20"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gkTrustAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(gkTrustAddr, "key_ids.0", regexp.MustCompile(`^kid-uploaded-`)),
					resource.TestCheckResourceAttrWith(gkTrustAddr, "guard_cluster_trust_id", equalTo(&trustID)),
				),
			},
			{
				// The key expires: the refresh drops it, and the apply makes
				// a new one with a new secret.
				PreConfig: func() { h.guard.expireKey(keyName) },
				Config:    h.guardK8sConfig("jwks = "+fmt.Sprintf("%q", gkJWKS), "max_nodes = 20"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gkKeyAddr, plancheck.ResourceActionCreate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(gkKeyAddr, "secret", notEqualTo(&secret)),
					resource.TestCheckResourceAttr(gkKeyAddr, "uses", "0"),
				),
			},
			{
				// The trust is revoked in the console: Terraform registers
				// the cluster again, and the rule follows its new trust.
				PreConfig: func() { h.guard.revokeTrust(trustName) },
				Config:    h.guardK8sConfig("jwks = "+fmt.Sprintf("%q", gkJWKS), "max_nodes = 20"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gkTrustAddr, plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gkRuleAddr, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttrWith(gkTrustAddr, "guard_cluster_trust_id", notEqualTo(&trustID)),
			},
			{
				ResourceName:      gkPodsAddr,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importByName(gkPodsAddr),
				Config:            h.guardK8sConfig("jwks = "+fmt.Sprintf("%q", gkJWKS), "max_nodes = 20"),
			},
			{
				ResourceName:      gkRuleAddr,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importByName(gkRuleAddr),
				Config:            h.guardK8sConfig("jwks = "+fmt.Sprintf("%q", gkJWKS), "max_nodes = 20"),
			},
			{
				// A discovering trust imports with nothing to change. (An
				// uploaded one would plan its jwks again, which is never
				// read back.)
				Config: discover,
			},
			{
				ResourceName:      gkTrustAddr,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importByName(gkTrustAddr),
				Config:            discover,
			},
			{
				ResourceName:      gkMembersAddr,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importByName(gkAdminsAddr),
				Config:            discover,
			},
			{
				// Joined nodes stay when their key or trust goes; an
				// administrator removes them, here before the network is
				// destroyed.
				// The key that expired in step 5 left state unrevoked; it no
				// longer holds the group, so the destroy below deletes it.
				PreConfig: func() { h.guard.dropNodes(joined...) },
				Config:    discover,
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			h.guard.mu.Lock()
			defer h.guard.mu.Unlock()
			if n := len(h.guard.k8s.groups); n != 0 {
				return fmt.Errorf("%d node groups left", n)
			}
			if n := len(h.guard.k8s.rules); n != 0 {
				return fmt.Errorf("%d rules left", n)
			}
			for name, tr := range h.guard.k8s.trusts {
				if tr.GetRevokedAt() == nil {
					return fmt.Errorf("trust %s not revoked", name)
				}
			}
			for name, k := range h.guard.k8s.keys {
				if k.GetRevokedAt() == nil && name != keyName {
					return fmt.Errorf("key %s not revoked", name)
				}
			}
			return nil
		},
	})
}

func TestAccGuardK8s_Refusals(t *testing.T) {

	h := newHarness(t)
	network := h.providerConfig() + guardTenantHCL + `
resource "authwise_guard_network" "office" {
  tenant_id    = local.guard_tenant_id
  display_name = "Office"
}
`
	trust := func(keys string) string {
		return network + fmt.Sprintf(`
resource "authwise_guard_cluster_trust" "c" {
  tenant_id    = local.guard_tenant_id
  network_id   = authwise_guard_network.office.guard_network_id
  display_name = "c"
  issuer       = %q
  %s
}
`, gkIssuer, keys)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: trust(""), PlanOnly: true, ExpectError: regexp.MustCompile(`the trust needs its keys`)},
			{Config: trust("discover = true\n  jwks = \"{}\""), PlanOnly: true, ExpectError: regexp.MustCompile(`discover and jwks are exclusive`)},
			{Config: trust("discover = false"), PlanOnly: true, ExpectError: regexp.MustCompile(`discover = false is not a choice`)},
			{
				Config: network + `
resource "authwise_guard_enrollment_key" "k" {
  tenant_id  = local.guard_tenant_id
  network_id = authwise_guard_network.office.guard_network_id
  ephemeral  = true
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`a one-use key cannot be ephemeral`),
			},
			{
				Config: network + `
resource "authwise_guard_enrollment_key" "k" {
  tenant_id   = local.guard_tenant_id
  network_id  = authwise_guard_network.office.guard_network_id
  expiry_days = 91
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`expiry_days`),
			},
		},
	})
}

// A key kept past its life with recreate_if_invalid = false stays in state,
// saying why; destroy revokes it.
func TestAccGuardK8s_KeyKeptWhenInvalid(t *testing.T) {

	h := newHarness(t)
	var keyName, secret, joined string
	config := h.providerConfig() + guardTenantHCL + `
resource "authwise_guard_network" "office" {
  tenant_id    = local.guard_tenant_id
  display_name = "Office"
}

resource "authwise_guard_enrollment_key" "k" {
  tenant_id           = local.guard_tenant_id
  network_id          = authwise_guard_network.office.guard_network_id
  recreate_if_invalid = false
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_guard_enrollment_key.k", "max_uses", "1"),
					resource.TestCheckResourceAttr("authwise_guard_enrollment_key.k", "ephemeral", "false"),
					resource.TestCheckResourceAttrWith("authwise_guard_enrollment_key.k", "name", func(v string) error { keyName = v; return nil }),
					resource.TestCheckResourceAttrWith("authwise_guard_enrollment_key.k", "secret", func(v string) error { secret = v; return nil }),
				),
			},
			{
				PreConfig: func() { joined = h.guard.joinWithKey(keyName, "server-1") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_guard_enrollment_key.k", "state", "USED_UP"),
					resource.TestCheckResourceAttrWith("authwise_guard_enrollment_key.k", "secret", equalTo(&secret)),
				),
			},
			{PreConfig: func() { h.guard.dropNodes(joined) }, Config: config},
		},
		CheckDestroy: func(_ *terraform.State) error {
			h.guard.mu.Lock()
			defer h.guard.mu.Unlock()
			if h.guard.k8s.keys[keyName].GetRevokedAt() == nil {
				return fmt.Errorf("key %s not revoked", keyName)
			}
			return nil
		},
	})
}
