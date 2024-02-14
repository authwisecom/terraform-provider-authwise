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
					resource.TestCheckResourceAttr("authwise_realm.test", "name", "one"),
					resource.TestCheckResourceAttr("authwise_realm.test", "tenant_id", ""),
					resource.TestCheckResourceAttr("authwise_realm.test", "description", ""),
					resource.TestCheckResourceAttr("authwise_realm.test", "user_database_type", ""),
				),
			},
			// ImportState testing
			{
				ResourceName:      "authwise_realm.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccRealmResourceConfig("two"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("authwise_realm.test", "name", "two"),
					resource.TestCheckResourceAttr("authwise_realm.test", "tenant_id", ""),
					resource.TestCheckResourceAttr("authwise_realm.test", "description", ""),
					resource.TestCheckResourceAttr("authwise_realm.test", "user_database_type", ""),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccRealmResourceConfig(configurableAttribute string) string {
	return fmt.Sprintf(`
resource "authwise_realm" "test" {
  name = %[1]q
}
`, configurableAttribute)
}
