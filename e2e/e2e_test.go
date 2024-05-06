// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSimpleE2E(t *testing.T) {
	funcs := NewSimpleE2ETestFuncs()

	var config string
	var testCheckFuncs []resource.TestCheckFunc
	for _, v := range funcs {
		config = config + v.resource()
		testCheckFuncs = append(testCheckFuncs, v.check(t))
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + config,
				Check: resource.ComposeAggregateTestCheckFunc(
					testCheckFuncs...,
				),
			},
		},
	})
}

func NewSimpleE2ETestFuncs() map[string]TestFunc {
	theme := &TestSimpleThemeFunc{
		name:                 randomString(8),
		stylesheet:           randomString(8),
		stylesheetAttributes: "",
		content:              "",
	}
	appearanceProfile := &TestSimpleAppearanceProfileFunc{
		name:                 randomString(8),
		themeIdentifier:      theme.resourceName() + ".id",
		stylesheetAttributes: "",
		content:              "",
	}
	audience := &TestSimpleAudienceFunc{
		name:                        randomString(8),
		appearanceProfileIdentifier: appearanceProfile.resourceName() + ".id",
		description:                 randomString(24),
		config:                      "",
	}

	client := &TestSimpleClientFunc{
		audienceIdentifier: audience.resourceName() + ".id",
		name:               randomString(8),
		grantType:          "client_credentials",
		config: `
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
`,
	}
	realm := &TestSimpleRealmFunc{
		name:             randomString(8),
		userDatabaseType: randomString(8),
	}
	provider := &TestSimpleProviderFunc{
		realmIdentifier: realm.resourceName() + ".id",
		name:            randomString(8),
		providerType:    "client_credentials",
		config:          "",
	}
	scope := &TestSimpleScopeFunc{
		audienceIdentifier: audience.resourceName() + ".id",
		kind:               randomString(8),
		auto:               "false",
		id:                 randomString(8),
	}
	role := &TestSimpleRoleFunc{
		name:               randomString(8),
		auto:               "false",
		audienceIdentifier: audience.resourceName() + ".id",
	}
	permission := &TestSimplePermissionFunc{
		audienceIdentifier: audience.resourceName() + ".id",
		name:               randomString(8),
	}

	return map[string]TestFunc{
		"theme":              theme,
		"appearance_profile": appearanceProfile,
		"audience":           audience,
		"client":             client,
		"realm":              realm,
		"provider":           provider,
		"scope":              scope,
		"role":               role,
		"permission":         permission,
	}
}
