// Command docsubcategory fills the subcategory of every page tfplugindocs
// generated under docs/resources and docs/data-sources, from
// provider.Subcategory (#31). tfplugindocs takes a subcategory only from a
// per-type template; this writes it into the generated frontmatter instead,
// so no type needs a template for its group alone. `make docs` runs it
// after `tfplugindocs generate`.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/provider"
)

func main() {
	if err := run("docs"); err != nil {
		fmt.Fprintln(os.Stderr, "docsubcategory:", err)
		os.Exit(1)
	}
}

func run(root string) error {

	for _, dir := range []string{"resources", "data-sources"} {

		pages, err := filepath.Glob(filepath.Join(root, dir, "*.md"))
		if err != nil {
			return err
		}

		for _, page := range pages {

			typeName := "authwise_" + strings.TrimSuffix(filepath.Base(page), ".md")
			group := provider.Subcategory(typeName)
			if group == "" {
				return fmt.Errorf("%s: %s has no subcategory; add it to provider.Subcategory", page, typeName)
			}

			body, err := os.ReadFile(page) //nolint:gosec // the pages tfplugindocs just wrote
			if err != nil {
				return err
			}
			const empty = "\nsubcategory: \"\"\n"
			if !strings.Contains(string(body), empty) {
				return fmt.Errorf("%s: no empty subcategory to fill", page)
			}
			out := strings.Replace(string(body), empty, fmt.Sprintf("\nsubcategory: %q\n", group), 1)
			if err := os.WriteFile(page, []byte(out), 0o600); err != nil { //nolint:gosec // the pages tfplugindocs just wrote
				return err
			}
		}
	}

	return nil
}
