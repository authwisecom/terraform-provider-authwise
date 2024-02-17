package main

import (
	"os"
	"path/filepath"
	"terraform-provider-authwise/gen/provider"
	"terraform-provider-authwise/gen/types"
	"text/template"
)

func main() {
	writeTemplates(provider.MakeResourceInput())
}

func writeTemplates(i *types.TemplateResourceInput) {
	for _, r := range i.Resources {
		t := template.New("main")

		t, err := t.Parse(i.TemplateContent)
		if err != nil {
			panic(err)
		}

		f, err := os.Create(filepath.Join(i.OutputPath, r.OutputFileName))
		if err != nil {
			panic(err)
		}

		err = t.ExecuteTemplate(f, "main", r)
		if err != nil {
			panic(err)
		}

	}
}
