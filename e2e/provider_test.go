// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSimpleProvider(t *testing.T) {
	params := newTestSimpleProviderParams()
	providerName := "authwise_provider.default"
	expectedConfig := `{}`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccProviderResource(params),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(providerName, "name", params.providerName),
					resource.TestCheckResourceAttr(providerName, "provider_type", "client_credentials"),
					testAccJSONConfig(t, providerName, "config", expectedConfig),
				),
			},
		},
	})
}

type testSimpleProviderParams struct {
	providerName string
	realmName    string
}

func newTestSimpleProviderParams() *testSimpleProviderParams {
	return &testSimpleProviderParams{
		providerName: randomString(8),
		realmName:    randomString(8),
	}
}

// TODO - computed user database type???
// TODO - backend not saving description field of realm
func testAccProviderResource(params *testSimpleProviderParams) string {
	return fmt.Sprintf(`

resource "authwise_realm" "default" {
  name = %[1]q
  description = "test realm"
  user_database_type = "mysql"
}

resource "authwise_provider" "default" {
  name = %[2]q
  provider_type = "client_credentials"
  realm_id = authwise_realm.default.id
  config = <<EOF
{}
EOF
}
`, params.realmName, params.providerName)
}
