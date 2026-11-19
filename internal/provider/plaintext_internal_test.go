package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/stretchr/testify/assert"
)

// Where the credentials would travel unencrypted: a warning for plaintext
// gRPC to anything but loopback, a refusal for an http token_url unless
// insecure is set, and a warning then too (audit, register entry for
// insecure and token_url).
func TestCheckPlaintext(t *testing.T) {

	type want struct{ errors, warnings int }

	cases := map[string]struct {
		insecure bool
		tokenURL string
		endpoint string
		guard    string
		want     want
	}{
		"tls and https":                 {tokenURL: "https://auth.example.com/oauth/token", endpoint: "api.example.com:443", want: want{}},
		"tls and the local store":       {endpoint: "api.example.com:443", want: want{}},
		"insecure to loopback":          {insecure: true, tokenURL: "http://127.0.0.1:8080/oauth/token", endpoint: "127.0.0.1:9090", guard: "localhost:9091", want: want{}},
		"insecure to ipv6 loopback":     {insecure: true, endpoint: "[::1]:9090", want: want{}},
		"insecure to a cluster service": {insecure: true, endpoint: "dns:///kit.authwise.svc:9090", want: want{warnings: 1}},
		"insecure to both remote":       {insecure: true, endpoint: "api.example.com:80", guard: "guard.example.com:80", want: want{warnings: 2}},
		"http token_url refused":        {tokenURL: "http://auth.example.com/oauth/token", endpoint: "api.example.com:443", want: want{errors: 1}},
		"http token_url, insecure":      {insecure: true, tokenURL: "http://auth.example.com/oauth/token", endpoint: "127.0.0.1:9090", want: want{warnings: 1}},
		"not a URL":                     {tokenURL: "auth.example.com/oauth/token", endpoint: "api.example.com:443", want: want{errors: 1}},
		"another scheme":                {tokenURL: "ftp://auth.example.com/token", endpoint: "api.example.com:443", want: want{errors: 1}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			checkPlaintext(tc.insecure, tc.tokenURL, map[string]string{"endpoint": tc.endpoint, "guard_endpoint": tc.guard}, &diags)
			assert.Equal(t, tc.want, want{diags.ErrorsCount(), diags.WarningsCount()}, diags)
		})
	}
}
