// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSimpleAudience(t *testing.T) {
	params := newTestSimpleAudienceParams()
	audienceName := "authwise_audience.default"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccAudienceResource(params),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(audienceName, "name", params.audienceName),
					resource.TestCheckResourceAttr(audienceName, "description", "test description"),
					resource.TestCheckResourceAttrSet(audienceName, "id"),
				),
			},
		},
	})
}

type testSimpleAudienceParams struct {
	audienceName string
}

func newTestSimpleAudienceParams() *testSimpleAudienceParams {
	return &testSimpleAudienceParams{
		audienceName: randomString(8),
	}
}

func testAccAudienceResource(params *testSimpleAudienceParams) string {
	return fmt.Sprintf(`

resource "authwise_audience" "default" {
  name = %[1]q
  description = "test description"
}
`, params.audienceName)
}
