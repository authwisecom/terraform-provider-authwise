// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSimpleClientInlineConfig(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccClientResourceConfig(newTestSimpleClientParams()),
				Check:  resource.ComposeAggregateTestCheckFunc(
				//resource.TestCheckResourceAttr("authwise_client.test", "name", "one"),
				),
			},
			// ImportState testing
			/*
				{
					ResourceName:      "authwise_client.test",
					ImportState:       true,
					ImportStateVerify: true,
				},

			*/
			/*
				// Update and Read testing
				{
					Config: testAccClientResourceConfig("two"),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("authwise_client.test", "name", "two"),
						resource.TestCheckResourceAttr("authwise_client.test", "alias", ""),
						resource.TestCheckResourceAttr("authwise_client.test", "login_url", ""),
						resource.TestCheckResourceAttr("authwise_client.test", "grant_type", ""),
						resource.TestCheckResourceAttr("authwise_client.test", "logo_id", ""),
						resource.TestCheckResourceAttr("authwise_client.test", "audience_id", ""),
						resource.TestCheckResourceAttr("authwise_client.test", "appearance_profile_id", ""),
					),
				},
			*/
			// Delete tests
		},
	})
}

type testSimpleClientParams struct {
	realmName    string
	audienceName string
	clientName   string
}

func newTestSimpleClientParams() *testSimpleClientParams {
	return &testSimpleClientParams{
		realmName:    randomString(8),
		audienceName: randomString(8),
		clientName:   randomString(8),
	}
}

func testAccClientResourceConfig(params *testSimpleClientParams) string {
	return fmt.Sprintf(`
resource "authwise_realm" "default" {
  name = %[1]q
}

resource "authwise_audience" "default" {
  name = %[2]q
}

resource "authwise_client" "default" {
  realm_id = authwise_realm.default.id
  audience_id = authwise_audience.default.id
  name = %[3]q
}
`, params.realmName, params.audienceName, params.clientName)
}
