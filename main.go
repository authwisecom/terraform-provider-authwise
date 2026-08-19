// The terraform-provider-authwise entry point: serves the tfinfra-generated
// Authwise provider over the Terraform plugin protocol (v6).
package main

import (
	"context"
	"flag"
	"log"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is stamped via -ldflags at build time (make build / CI).
var version = "dev"

func main() {

	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/authwisecom/authwise",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
