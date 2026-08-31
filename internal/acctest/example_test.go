package acctest_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The shipped examples are applied by the real terraform CLI against the real
// provider binary here, rather than through terraform-plugin-testing, because
// the plugin-testing harness shims state through the legacy SDK
// representation and that shim cannot express for_each string keys
// ("unexpected index type (string) ... for_each is not supported"). A catalog
// keyed by permission name is exactly the right idiom for the configuration
// an operator writes — stable addressing as the list grows — so the example
// keeps for_each and the test meets it where it is.

// exampleRun builds the provider, points terraform at it with a dev override,
// and applies the named example directory against the fake stack.
type exampleRun struct {
	t       *testing.T
	dir     string
	env     []string
	applied bool
}

func newExampleRun(t *testing.T, h *harness, example string) *exampleRun {

	t.Helper()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatal(err)
	}

	build := exec.CommandContext(t.Context(), "go", "build",
		"-o", filepath.Join(binDir, "terraform-provider-authwise"), ".")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the provider: %v\n%s", err, out)
	}

	// A dev override binds the source address to the freshly built binary,
	// so the run needs no registry, no lock file and no init.
	cliConfig := filepath.Join(root, "terraform.rc")
	err := os.WriteFile(cliConfig, []byte(fmt.Sprintf(`
provider_installation {
  dev_overrides {
    "registry.terraform.io/authwisecom/authwise" = %q
  }
  direct {}
}
`, binDir)), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	// The example's own resources, verbatim; only the provider block is
	// replaced, to point at the fake stack instead of a real install.
	work := filepath.Join(root, "config")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join("../../examples", example, "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "main.tf"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	provider := fmt.Sprintf(`
terraform {
  required_providers {
    authwise = {
      source = "registry.terraform.io/authwisecom/authwise"
    }
  }
}
%s
`, h.providerConfig())
	if err := os.WriteFile(filepath.Join(work, "provider.tf"), []byte(provider), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &exampleRun{
		t:   t,
		dir: work,
		env: append(os.Environ(),
			"TF_CLI_CONFIG_FILE="+cliConfig,
			"TF_IN_AUTOMATION=1",
			"TF_INPUT=0",
		),
	}

	t.Cleanup(func() {
		if !r.applied {
			return
		}
		// t.Context() is cancelled before cleanups run, so the teardown
		// destroy gets its own context.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		r.runContext(ctx, "destroy", "-auto-approve")
	})

	return r
}

// run executes one terraform command in the example's working directory.
func (r *exampleRun) run(args ...string) string {
	r.t.Helper()
	return r.runContext(r.t.Context(), args...)
}

func (r *exampleRun) runContext(ctx context.Context, args ...string) string {

	r.t.Helper()

	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = r.dir
	cmd.Env = r.env

	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("terraform %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	return string(out)
}

// apply applies the example and records that a destroy is owed.
func (r *exampleRun) apply() string {
	r.t.Helper()
	r.applied = true
	return r.run("apply", "-auto-approve")
}

// expectCleanPlan fails unless terraform reports no changes: exit code 0 from
// -detailed-exitcode, where 2 means a diff.
func (r *exampleRun) expectCleanPlan() {

	r.t.Helper()

	cmd := exec.CommandContext(r.t.Context(), "terraform", "plan", "-detailed-exitcode")
	cmd.Dir = r.dir
	cmd.Env = r.env

	out, err := cmd.CombinedOutput()
	if err == nil {
		return
	}

	var exit *exec.ExitError
	if ok := asExitError(err, &exit); ok && exit.ExitCode() == 2 {
		r.t.Fatalf("re-plan is not clean:\n%s", out)
	}
	r.t.Fatalf("terraform plan: %v\n%s", err, out)
}

func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError) //nolint:errorlint // the exit code is the whole point
	if ok {
		*target = e
	}
	return ok
}

// TestAccGuardCatalogExample applies examples/guard-catalog verbatim: the
// whole guard prerequisite — 31 permissions, the role, the authoritative
// association, the root-anchored binding, and the console audience and
// client — then asserts a re-plan is empty.
func TestAccGuardCatalogExample(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	h := newHarness(t)
	r := newExampleRun(t, h, "guard-catalog")

	r.apply()
	r.expectCleanPlan()

	h.access.mu.Lock()
	permissions, bindings := len(h.access.permissions), len(h.access.bindings)
	h.access.mu.Unlock()

	if permissions != 31 {
		t.Errorf("%d access permissions on the server, want 31", permissions)
	}
	if bindings != 1 {
		t.Errorf("%d access bindings, want 1", bindings)
	}

	members := h.access.rolePermissions(accessPrefix + "/access-roles/guardcontrol.admin")
	if len(members) != 31 {
		t.Errorf("the role carries %d permissions, want 31", len(members))
	}

	b := h.access.onlyBinding()
	if b == nil || b.GetRoleName() != "guardcontrol.admin" || b.GetSubjectId() != "u-01" {
		t.Errorf("binding = %v", b)
	}

	// The console pair: the client must reference the audience this run
	// created, by AWID rather than by full name.
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()

	if len(h.fake.audiences) != 1 || len(h.fake.clients) != 1 {
		t.Fatalf("console pair: %d audiences, %d clients", len(h.fake.audiences), len(h.fake.clients))
	}

	var audienceID string
	for name, a := range h.fake.audiences {
		audienceID = name[strings.LastIndex(name, "/")+1:]
		if a.GetDisplayName() != "https://guard.example.com/api" {
			t.Errorf("audience display_name = %q", a.GetDisplayName())
		}
	}

	for _, c := range h.fake.clients {
		if c.GetAudienceId() != audienceID {
			t.Errorf("client audience_id = %q, want the created audience %q", c.GetAudienceId(), audienceID)
		}
		if c.GetGrantType() != "authorization_code" {
			t.Errorf("client grant_type = %q", c.GetGrantType())
		}
		if !strings.Contains(string(c.GetConfig().GetValue()), "guard-admin.example.com/") {
			t.Errorf("client config does not carry the redirect URI: %s", c.GetConfig())
		}
	}
}
