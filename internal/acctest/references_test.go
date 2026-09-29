// References read a leaf attribute, never a split (#26): every resource
// exports its own id as <type>_id, a reference attribute takes it, and one
// handed a full name fails in plan with the attribute to use. kit#620
// refuses the same at apply, and an audience of another issuer too; the
// fake admits Client.audience_id the same way.
package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// checkIDIsLastSegment asserts that addr's idAttr is the last segment of its
// name and carries the type's prefix.
func checkIDIsLastSegment(addr, idAttr, prefix string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s not in state", addr)
		}
		name, id := rs.Primary.Attributes["name"], rs.Primary.Attributes[idAttr]
		if want := name[strings.LastIndex(name, "/")+1:]; id != want {
			return fmt.Errorf("%s.%s = %q, want the last segment of %q", addr, idAttr, id, name)
		}
		if !strings.HasPrefix(id, prefix+"-") {
			return fmt.Errorf("%s.%s = %q, want the %q prefix", addr, idAttr, id, prefix)
		}
		return nil
	}
}

func TestAccReferences_ByID(t *testing.T) {

	h := newHarness(t)

	config := func(audienceID string) string {
		return h.providerConfig() + fmt.Sprintf(`
resource "authwise_audience" "api" {
  display_name = "API"
}

resource "authwise_client" "web" {
  display_name = "web"
  grant_type   = "client_credentials"
  audience_id  = %s
}

data "authwise_audience" "api" {
  name = authwise_audience.api.name
}
`, audienceID)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("authwise_audience.api.audience_id"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkIDIsLastSegment("authwise_audience.api", "audience_id", "a"),
					checkIDIsLastSegment("authwise_client.web", "client_id", "c"),
					checkIDIsLastSegment("data.authwise_audience.api", "audience_id", "a"),
					resource.TestCheckResourceAttrPair("authwise_client.web", "audience_id", "authwise_audience.api", "audience_id"),
				),
			},
			{
				// The mistake the attribute exists to prevent: the name, not
				// the id. It fails in plan, naming the attribute to use.
				Config:      config("authwise_audience.api.name"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)is\s+a\s+resource\s+name,\s+not\s+an\s+id.*authwise_audience\.<name>\.audience_id`),
			},
			{
				// Another type's id fails in plan too.
				Config:      config(`"c-00000002"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`takes the audience's id, which starts with\s+"a-"`),
			},
			{
				// A split written before #26 still yields the id: no diff.
				// Last, so the post-test destroy runs a valid config.
				Config:   config(`element(split("/", authwise_audience.api.name), 5)`),
				PlanOnly: true,
			},
		},
	})
}

// An audience of another issuer passes the plan-time check, since it is an
// audience id, and kit refuses it at apply (kit#620).
func TestAccReferences_OutOfScopeRefusedAtApply(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.providerConfig() + `
resource "authwise_audience" "api" {
  display_name = "API"
}

resource "authwise_client" "elsewhere" {
  issuer_id    = "i-2"
  display_name = "elsewhere"
  grant_type   = "client_credentials"
  audience_id  = authwise_audience.api.audience_id
}
`,
				ExpectError: regexp.MustCompile(`audience_id: no audience a-\d+\s+in\s+scope`),
			},
		},
	})
}

// A parent attribute is validated like any other reference.
func TestAccReferences_ParentValidated(t *testing.T) {

	h := newHarness(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.providerConfig() + `
resource "authwise_realm" "r" {
  tenant_id    = "tenants/t-1"
  display_name = "r"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`tenant_id takes the tenant's id, which starts with\s+"t-":\s+"tenants/t-1"\s+is\s+a\s+resource\s+name`),
			},
		},
	})
}
