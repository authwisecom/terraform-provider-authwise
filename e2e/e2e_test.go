// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSimpleClientInlineConfig(t *testing.T) {
	params := newTestSimpleClientParams()
	clientName := "authwise_client.default"
	expectedClientConfig := &ClientConfig{
		accessTokenExpireSeconds: 7200,
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccClientResourceConfig(params),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(clientName, "name", params.clientName),
					resource.TestCheckResourceAttr(clientName, "grant_type", "client_credentials"),
					resource.TestCheckResourceAttrSet(clientName, "audience_id"),
					testAccCheckClientConfiguration(clientName, "config", expectedClientConfig),
				),
			},
			// ImportState testing
			//{
			//	ResourceName:      clientName,
			//	ImportState:       true,
			//	ImportStateVerify: true,
			//},
			// Update and Read testing
			//{
			//	Config: testAccClientResourceConfig(newTestSimpleClientParams()),
			//	Check: resource.ComposeAggregateTestCheckFunc(
			//		resource.TestCheckResourceAttr("authwise_client.test", "name", "two"),
			//		resource.TestCheckResourceAttr("authwise_client.test", "alias", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "login_url", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "grant_type", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "logo_id", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "audience_id", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "appearance_profile_id", ""),
			//	),
			//},
			// Delete tests
		},
	})
}

func TestSimpleClientDataSourceConfig(t *testing.T) {
	params := newTestSimpleClientParams()
	clientName := "authwise_client.default"
	expectedClientConfig := &ClientConfig{
		accessTokenExpireSeconds: 7200,
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccClientResourceConfigWithDataSource(params),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(clientName, "name", params.clientName),
					resource.TestCheckResourceAttr(clientName, "grant_type", "client_credentials"),
					resource.TestCheckResourceAttrSet(clientName, "audience_id"),
					testAccCheckClientConfiguration(clientName, "config", expectedClientConfig),
				),
			},
			// ImportState testing
			//{
			//	ResourceName:      clientName,
			//	ImportState:       true,
			//	ImportStateVerify: true,
			//},
			// Update and Read testing
			//{
			//	Config: testAccClientResourceConfig(newTestSimpleClientParams()),
			//	Check: resource.ComposeAggregateTestCheckFunc(
			//		resource.TestCheckResourceAttr("authwise_client.test", "name", "two"),
			//		resource.TestCheckResourceAttr("authwise_client.test", "alias", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "login_url", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "grant_type", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "logo_id", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "audience_id", ""),
			//		resource.TestCheckResourceAttr("authwise_client.test", "appearance_profile_id", ""),
			//	),
			//},
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

resource "authwise_audience" "default" {
  name = %[2]q
}

resource "authwise_client" "default" {
  audience_id = authwise_audience.default.id
  grant_type = "client_credentials"
  config = <<EOF
{
	"@type": "type.googleapis.com/authwise.types.core.v1alpha1.InteractiveClientConfig",
	"access_token_expire_seconds": 7200,
	"allowed_redirect_uris": ["http://localhost:8990/return","https://localhost:3000*"],
	"cors": {
		"allowed_origins": ["https://localhost:3000"],
		"options_mode": "CORS_OPTIONS_MODE_STRICT"
	},
	"interaction_forward_uri": "/"
  }
EOF
  name = %[3]q
}
`, params.realmName, params.audienceName, params.clientName)
}

func testAccClientResourceConfigWithDataSource(params *testSimpleClientParams) string {
	return fmt.Sprintf(`
resource "authwise_audience" "default" {
  name = %[2]q
}

data "authwise_interactive_client_config" "default" {
	access_token_expire_seconds = 7200
	allowed_redirect_uris = ["http://localhost:8990/return","https://localhost:3000*"]
	cors = {
		"allowed_origins" = ["https://localhost:3000"]
		"options_mode" = 1
	}
	interaction_forward_uri = "/"
}

resource "authwise_client" "default" {
  audience_id = authwise_audience.default.id
  grant_type = "client_credentials"
  name = %[3]q
  config = data.authwise_interactive_client_config.default.result
}
`, params.realmName, params.audienceName, params.clientName)
}
