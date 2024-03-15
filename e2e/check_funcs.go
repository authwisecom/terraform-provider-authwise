package e2e

import (
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCheckClientConfiguration(resourceName string, key string, expected *ClientConfig) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		//rs, ok := s.RootModule().Resources[resourceName]
		//if !ok {
		//	return fmt.Errorf("not found: %s", resourceName)
		//}
		//fmt.Printf("test: %v", rs.Primary)

		return nil
	}
}
