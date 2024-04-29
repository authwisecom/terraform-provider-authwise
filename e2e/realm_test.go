// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSimpleRealm(t *testing.T) {
	params := newTestSimpleRealmParams()
	realmName := "authwise_realm.default"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccRealmResource(params),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(realmName, "name", params.realmName),
					resource.TestCheckResourceAttr(realmName, "user_database_type", "mysql"),
				),
			},
		},
	})
}

type testSimpleRealmParams struct {
	realmName string
}

func newTestSimpleRealmParams() *testSimpleRealmParams {
	return &testSimpleRealmParams{
		realmName: randomString(8),
	}
}

// TODO - computed user database type???
func testAccRealmResource(params *testSimpleRealmParams) string {
	return fmt.Sprintf(`

resource "authwise_realm" "default" {
  name = %[1]q
  user_database_type = "mysql"
}

`, params.realmName)
}
