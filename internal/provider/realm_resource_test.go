// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccRealmResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccRealmResourceConfig("one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_realm.test", "configurable_attribute", "one"),
					resource.TestCheckResourceAttr("authwise_realm.test", "defaulted", "realm value when not configured"),
					resource.TestCheckResourceAttr("authwise_realm.test", "id", "realm-id"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "authwise_realm.test",
				ImportState:       true,
				ImportStateVerify: true,
				// This is not normally necessary, but is here because this
				// realm code does not have an actual upstream service.
				// Once the Read method is able to refresh information from
				// the upstream service, this can be removed.
				ImportStateVerifyIgnore: []string{"configurable_attribute", "defaulted"},
			},
			// Update and Read testing
			{
				Config: testAccRealmResourceConfig("two"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_realm.test", "configurable_attribute", "two"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccRealmResourceConfig(configurableAttribute string) string {
	return fmt.Sprintf(`
resource "authwise_realm" "test" {
  configurable_attribute = %[1]q
}
`, configurableAttribute)
}
