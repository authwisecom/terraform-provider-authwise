package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/require"
	"testing"
)

func testAccJSONConfig(t *testing.T, resourceName string, key string, expected string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if expected == "" {
			return nil
		}
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource not found: %s", resourceName)
		}
		j, ok := rs.Primary.Attributes[key]
		if !ok {
			return fmt.Errorf("key not found: %s", key)
		}
		expectedJson := new(bytes.Buffer)

		err := json.Compact(expectedJson, []byte(expected))
		if err != nil {
			return fmt.Errorf("invalid json")
		}

		require.JSONEq(t, expectedJson.String(), j)

		return nil
	}
}
