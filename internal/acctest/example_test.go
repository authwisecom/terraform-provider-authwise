package acctest_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
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

// Each of beside's examples is copied in too, its files prefixed with its
// directory name: examples meant to be applied as one configuration.
func newExampleRun(t *testing.T, h *harness, example string, beside ...string) *exampleRun {

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

	// The example's own files, verbatim — main.tf and whatever it reads with
	// file(); only the provider block is replaced, to point at the fake
	// stack instead of a real install.
	work := filepath.Join(root, "config")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}

	for i, dir := range append([]string{example}, beside...) {
		prefix := ""
		if i > 0 {
			prefix = dir + "-"
		}
		src := filepath.Join("../../examples", dir)
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "provider.tf" || e.Name() == "README.md" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(work, prefix+e.Name()), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
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
// whole guard prerequisite — 37 permissions (guard-control v0.8.1), the role, the authoritative
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

	if permissions != 37 {
		t.Errorf("%d access permissions on the server, want 37", permissions)
	}
	if bindings != 1 {
		t.Errorf("%d access bindings, want 1", bindings)
	}

	members := h.access.rolePermissions(accessPrefix + "/access-roles/guardcontrol.admin")
	if len(members) != 37 {
		t.Errorf("the role carries %d permissions, want 37", len(members))
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
		if got := c.GetGrantTypes(); !slices.Equal(got, []string{"authorization_code", "refresh_token"}) {
			t.Errorf("client grant_types = %v", got)
		}
		if c.GetKind() != corepb.ClientKind_CLIENT_KIND_APPLICATION || c.GetTokenEndpointAuthMethod() != "none" {
			t.Errorf("client kind = %s, token_endpoint_auth_method = %q; want a public application", c.GetKind(), c.GetTokenEndpointAuthMethod())
		}
		if !strings.Contains(string(c.GetConfig().GetValue()), "guard-admin.example.com/") {
			t.Errorf("client config does not carry the redirect URI: %s", c.GetConfig())
		}
	}
}

// TestAccGuardExample applies examples/guard and examples/guard-catalog as
// one configuration (#32): kit's catalog, role, binding and console client
// beside a Guard tenant, network, relay, two resources and an invite.
func TestAccGuardExample(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	h := newHarness(t)
	r := newExampleRun(t, h, "guard", "guard-catalog")

	r.apply()
	r.expectCleanPlan()

	if url := strings.TrimSpace(r.run("output", "-raw", "invite_url")); !strings.HasPrefix(url, "https://join.example.com/i/gi_") {
		t.Errorf("invite_url = %q", url)
	}

	h.guard.mu.Lock()
	defer h.guard.mu.Unlock()
	if len(h.guard.tenants) != 1 || len(h.guard.networks) != 1 || len(h.guard.relays) != 1 ||
		len(h.guard.resources) != 2 || len(h.guard.invites) != 1 {
		t.Errorf("guard-control holds %d tenants, %d networks, %d relays, %d resources, %d invites; want 1, 1, 1, 2, 1",
			len(h.guard.tenants), len(h.guard.networks), len(h.guard.relays), len(h.guard.resources), len(h.guard.invites))
	}
	for _, i := range h.guard.invites {
		if len(i.GetGrants()) != 2 {
			t.Errorf("invite grants = %v, want both resources", i.GetGrants())
		}
	}
	if len(h.access.permissions) != 37 {
		t.Errorf("%d access permissions beside the network, want 37", len(h.access.permissions))
	}
}

// TestAccAuthnExample applies examples/authn verbatim, then disables the
// passkey factor the admins rule requires. kit accepts that write with a
// warning, which must reach the person running terraform twice: as a
// warning on the apply that caused it, and through the example's check
// block on every plan after.
func TestAccAuthnExample(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	h := newHarness(t)
	r := newExampleRun(t, h, "authn")

	if out := r.apply(); strings.Contains(out, "kit accepted the write with a warning") {
		t.Errorf("a clean apply warned:\n%s", out)
	}
	r.expectCleanPlan()

	h.fake.mu.Lock()
	factors, secrets := len(h.fake.factors), len(h.fake.secrets)
	h.fake.mu.Unlock()
	if factors != 3 || secrets != 1 {
		t.Fatalf("%d factors and %d secrets on the server, want 3 and 1", factors, secrets)
	}

	const want = `rule "admins", requires webauthn, which no active factor offers`

	out := r.run("apply", "-auto-approve", "-var", "passkeys_status=disabled")
	if !strings.Contains(out, "kit accepted the write with a warning") || !strings.Contains(out, want) {
		t.Errorf("the apply did not surface kit's warning:\n%s", out)
	}

	out = r.run("plan", "-var", "passkeys_status=disabled")
	if !strings.Contains(out, "Check block assertion failed") || !strings.Contains(out, want) {
		t.Errorf("the check block did not surface kit's warning:\n%s", out)
	}
}

// TestAccEndpointExamples applies each examples/endpoints directory verbatim
// and asserts a re-plan is empty — the proof that timeout keeps the spelling
// the example wrote ("1500ms" is stored as 1.5s) and that tls and auth read
// back as they went in.
func TestAccEndpointExamples(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	type s struct {
		assert func(t *testing.T, e *corepb.Endpoint, h *harness)
	}

	cases := map[string]s{
		"bearer": {
			assert: func(t *testing.T, e *corepb.Endpoint, h *harness) {
				if got := e.GetTimeout().AsDuration(); got != 2*time.Second {
					t.Errorf("timeout = %s, want 2s", got)
				}
				if _, ok := h.fake.secrets[e.GetAuth().GetBearer().GetToken().GetName()]; !ok {
					t.Errorf("bearer does not name the example's secret: %v", e.GetAuth())
				}
			},
		},
		"kit_token": {
			assert: func(t *testing.T, e *corepb.Endpoint, h *harness) {
				if got := e.GetTimeout().AsDuration(); got != 1500*time.Millisecond {
					t.Errorf("timeout = %s, want 1.5s", got)
				}
				kt := e.GetAuth().GetKitToken()
				if _, ok := h.fake.issuers[kt.GetIssuer()]; !ok || kt.GetAudience() != "https://ledger.example.com" {
					t.Errorf("kit_token = %v", kt)
				}
				if e.GetTls().GetServerName() != "ledger.internal" {
					t.Errorf("tls = %v", e.GetTls())
				}
			},
		},
		"mtls": {
			assert: func(t *testing.T, e *corepb.Endpoint, h *harness) {
				c, ok := h.fake.certs[e.GetTls().GetClientCertificate()]
				if !ok || !c.GetHasPrivateKey() {
					t.Errorf("tls.client_certificate = %q does not name the minted certificate", e.GetTls().GetClientCertificate())
				}
				if !strings.Contains(e.GetTls().GetCaPem(), "BEGIN CERTIFICATE") {
					t.Errorf("tls.ca_pem did not arrive: %q", e.GetTls().GetCaPem())
				}
				if e.GetAuth() != nil {
					t.Errorf("auth = %v, want none: the client certificate is not an auth scheme", e.GetAuth())
				}
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			h := newHarness(t)
			r := newExampleRun(t, h, "endpoints/"+k)

			r.apply()
			r.expectCleanPlan()

			h.fake.mu.Lock()
			defer h.fake.mu.Unlock()

			if len(h.fake.endpoints) != 1 {
				t.Fatalf("%d endpoints on the server, want 1", len(h.fake.endpoints))
			}
			for _, e := range h.fake.endpoints {
				v.assert(t, e, h)
			}
		})
	}
}

// TestAccPasswordlessExample applies examples/passwordless verbatim — a
// password-or-magic-link realm and a passkey realm beside its webauthn
// factor — then asserts a re-plan is empty, which is the proof that the
// magic link's ttl survives the round trip through the provider's Any.
func TestAccPasswordlessExample(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	h := newHarness(t)
	r := newExampleRun(t, h, "passwordless")

	r.apply()
	r.expectCleanPlan()

	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()

	types := map[string]int{}
	for _, p := range h.fake.providers {
		types[p.GetProviderType()]++
		if p.GetProviderType() == "magicLink" {
			ml := &corepb.ProviderMagicLink{}
			if err := p.GetConfig().UnmarshalTo(ml); err != nil || ml.GetTtl().AsDuration() != 10*time.Minute {
				t.Errorf("magic link config = %v (%v)", ml, err)
			}
		}
	}
	if types["usernamePassword"] != 1 || types["magicLink"] != 1 || types["passkey"] != 1 {
		t.Errorf("providers by type = %v", types)
	}
	if len(h.fake.factors) != 1 {
		t.Errorf("%d factors, want the passkey realm's webauthn factor", len(h.fake.factors))
	}
}

// TestAccIdentifierFirstExample applies examples/identifier-first verbatim
// and asserts a re-plan is empty: the selector JSON reads back as written.
func TestAccIdentifierFirstExample(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	h := newHarness(t)
	r := newExampleRun(t, h, "identifier-first")

	r.apply()
	r.expectCleanPlan()

	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()

	if len(h.fake.issuers) != 1 {
		t.Fatalf("%d issuers, want 1", len(h.fake.issuers))
	}
	for _, i := range h.fake.issuers {
		s := i.GetConfig().GetMultiRealmProviderSelector()
		if len(s.GetRealmNames()) != 2 || len(s.GetRules()) != 1 || s.GetDefaultTarget().GetRealmName() == "" {
			t.Errorf("selector = %v", s)
		}
		rule := s.GetRules()[0]
		if _, ok := h.fake.providers[rule.GetTarget().GetProviderName()]; !ok {
			t.Errorf("partner rule does not target the SAML provider: %v", rule)
		}
		if s.GetIdentifier().GetKind() != corepb.IdentifierField_KIND_UNSPECIFIED {
			t.Errorf("kind = %v, want it left unset (read as EMAIL at login)", s.GetIdentifier().GetKind())
		}
	}
}

// TestAccSocialExample applies examples/social verbatim — six providers,
// each with its secret held by reference — and asserts a re-plan is empty
// and that no provider row carries secret material.
func TestAccSocialExample(t *testing.T) {

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 to run")
	}

	h := newHarness(t)
	r := newExampleRun(t, h, "social")

	r.apply()
	r.expectCleanPlan()

	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()

	types := map[string]bool{}
	for _, p := range h.fake.providers {
		types[p.GetProviderType()] = true
		for _, ref := range secretRefs(p) {
			if _, ok := h.fake.secrets[ref]; !ok {
				t.Errorf("%s names secret %q, which does not exist", p.GetName(), ref)
			}
		}
		if strings.Contains(p.GetConfig().String(), "replace-me") {
			t.Errorf("%s carries secret material in its config", p.GetName())
		}
	}
	for _, want := range []string{"google", "microsoft", "github", "apple", "oidc", "oauth"} {
		if !types[want] {
			t.Errorf("no %s provider on the server; have %v", want, types)
		}
	}
	if len(h.fake.secrets) != 6 {
		t.Errorf("%d secrets, want 6", len(h.fake.secrets))
	}
}
