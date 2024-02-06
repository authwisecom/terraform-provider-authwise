terraform {
  required_providers {
    authwise = {
      source = "authwise.com/terraform/authwise"
    }
  }
}

provider "authwise" {}

data "authwise_coffees" "example" {}
