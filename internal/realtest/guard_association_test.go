// Package realtest_test runs the provider against a real kit and a real
// guard-control (tf#86): the released images in realserver/, started by
// realserver/up.sh or the realserver CI job. The fake in internal/acctest
// builds names by the rule its tests assert, so it can only confirm itself
// (guard-control#66 shipped through it); these tests take every name from
// the server.
//
// They run only when REALSERVER_CREDENTIAL names the stack's minted client
// file and TF_ACC is set; anywhere else they skip.
package realtest_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// realTenant is the stack's seeded tenant (realserver/kit/static-data.yaml).
const realTenant = "t-01"

func factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"authwise": providerserver.NewProtocol6WithError(provider.New("realtest")()),
	}
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// providerHCL configures the provider for the stack from the minted
// credential, skipping the test when there is no stack.
func providerHCL(t *testing.T) string {
	t.Helper()
	path := os.Getenv("REALSERVER_CREDENTIAL")
	if path == "" || os.Getenv("TF_ACC") == "" {
		t.Skip("no real-server stack: set TF_ACC and REALSERVER_CREDENTIAL (realserver/up.sh)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the stack's credential: %v", err)
	}
	var cred struct {
		TokenURL     string `json:"token_url"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Audience     string `json:"audience"`
	}
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatalf("reading the stack's credential: %v", err)
	}
	return fmt.Sprintf(`
provider "authwise" {
  endpoint       = %q
  guard_endpoint = %q
  insecure       = true
  token_url      = %q
  client_id      = %q
  client_secret  = %q
  audience       = %q
  tenant_id      = %q
}
`, env("REALSERVER_KIT_API", "127.0.0.1:28444"), env("REALSERVER_GUARD_CONTROL", "127.0.0.1:28445"),
		cred.TokenURL, cred.ClientID, cred.ClientSecret, cred.Audience, realTenant)
}

// wgKey is a fresh WireGuard-shaped public key: 32 bytes, base64.
func wgKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// association is one authoritative Guard association resource: its type,
// the attribute naming its entity and its member set, and the members by
// Terraform address.
type association struct {
	typeName  string
	entity    string // e.g. "guard_resource = authwise_guard_resource.r.name"
	attribute string
	all       []string // members after the first apply
	fewer     []string // members after one is removed
}

func (a association) addr() string { return a.typeName + ".under_test" }

func (a association) hcl(members []string) string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m + ".name"
	}
	return fmt.Sprintf(`
resource %q "under_test" {
  %s
  %s = [%s]
}
`, a.typeName, a.entity, a.attribute, strings.Join(names, ", "))
}

// checkMembers compares the stored set with the members' own names, whole:
// each stored name must be exactly a member's `name` as guard-control
// returned it on create, never a name the provider or a fake composed.
func (a association) checkMembers(members []string) resource.TestCheckFunc {
	checks := make([]resource.TestCheckFunc, 0, 1+len(members))
	checks = append(checks, resource.TestCheckResourceAttr(a.addr(), a.attribute+".#", fmt.Sprint(len(members))))
	for _, m := range members {
		checks = append(checks, resource.TestCheckTypeSetElemAttrPair(a.addr(), a.attribute+".*", m, "name"))
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
}

// run drives the four steps of tf#86: apply the set, plan clean, remove one
// member and apply, then destroy (the framework's teardown, which fails
// the test when it errors).
func (a association) run(t *testing.T, base string) {
	t.Helper()
	cfg := func(members []string) string { return providerHCL(t) + base + a.hcl(members) }
	empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{Config: cfg(a.all), Check: a.checkMembers(a.all), ConfigPlanChecks: empty},
			{Config: cfg(a.all), PlanOnly: true},
			{Config: cfg(a.fewer), Check: a.checkMembers(a.fewer), ConfigPlanChecks: empty},
		},
	})
}

// network is a Guard network with nodes n1..n3, groups g1 and g2 and a
// host resource r, every display name unique to the run.
func network(t *testing.T) string {
	t.Helper()
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	run := fmt.Sprintf("%s %x", strings.TrimPrefix(t.Name(), "TestReal_"), suffix)
	var b strings.Builder
	fmt.Fprintf(&b, `
resource "authwise_guard_network" "net" {
  tenant_id    = %q
  display_name = "tf86 %s"
}
`, realTenant, run)
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&b, `
resource "authwise_guard_node" "n%d" {
  tenant_id    = authwise_guard_network.net.tenant_id
  network_id   = authwise_guard_network.net.guard_network_id
  display_name = "n%d"
  public_key   = %q
}
`, i, i, wgKey(t))
	}
	for i := 1; i <= 2; i++ {
		fmt.Fprintf(&b, `
resource "authwise_guard_node_group" "g%d" {
  tenant_id    = authwise_guard_network.net.tenant_id
  network_id   = authwise_guard_network.net.guard_network_id
  display_name = "g%d"
}
`, i, i)
	}
	b.WriteString(`
resource "authwise_guard_resource" "r" {
  tenant_id    = authwise_guard_network.net.tenant_id
  network_id   = authwise_guard_network.net.guard_network_id
  display_name = "wiki"
  kind         = "host"
  address      = "10.0.0.5"
}
`)
	return b.String()
}

// TestReal_GuardResourceNodes is the resource from guard-control#66: its
// read once named each node under the resource's collection, so every plan
// changed the set and every apply failed.
func TestReal_GuardResourceNodes(t *testing.T) {
	association{
		typeName:  "authwise_guard_resource_nodes",
		entity:    "guard_resource = authwise_guard_resource.r.name",
		attribute: "nodes",
		all:       []string{"authwise_guard_node.n1", "authwise_guard_node.n2"},
		fewer:     []string{"authwise_guard_node.n1"},
	}.run(t, network(t))
}

func TestReal_GuardResourceNodeGroups(t *testing.T) {
	association{
		typeName:  "authwise_guard_resource_node_groups",
		entity:    "resource = authwise_guard_resource.r.name",
		attribute: "node_groups",
		all:       []string{"authwise_guard_node_group.g1", "authwise_guard_node_group.g2"},
		fewer:     []string{"authwise_guard_node_group.g1"},
	}.run(t, network(t))
}

func TestReal_GuardNodeGroupMembers(t *testing.T) {
	association{
		typeName:  "authwise_guard_node_group_members",
		entity:    "node_group = authwise_guard_node_group.g1.name",
		attribute: "nodes",
		all:       []string{"authwise_guard_node.n1", "authwise_guard_node.n2"},
		fewer:     []string{"authwise_guard_node.n1"},
	}.run(t, network(t))
}

func TestReal_GuardNodeGroupGrants(t *testing.T) {
	association{
		typeName:  "authwise_guard_node_group_grants",
		entity:    "node_group = authwise_guard_node_group.g1.name",
		attribute: "grants",
		all:       []string{"authwise_guard_node_group.g2", "authwise_guard_resource.r"},
		fewer:     []string{"authwise_guard_node_group.g2"},
	}.run(t, network(t))
}
