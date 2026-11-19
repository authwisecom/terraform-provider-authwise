package provider

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// The provider attributes naming the gRPC targets the bearer token goes to.
const (
	endpointAttr      = "endpoint"
	guardEndpointAttr = "guard_endpoint"
)

// checkPlaintext reports where the provider's credentials would travel
// unencrypted. insecure = true dials plaintext gRPC, and the bearer token
// rides on every call: it is allowed, since an in-cluster caller may dial a
// plaintext .svc address, but warned about for any host that is not
// loopback. An http token_url sends the client secret in the clear, so it
// is refused unless insecure is set, and then warned about the same way.
func checkPlaintext(insecure bool, tokenURL string, targets map[string]string, diags *diag.Diagnostics) {
	if insecure {
		warnPlaintextTargets(targets, diags)
	}
	checkTokenURL(insecure, tokenURL, diags)
}

// warnPlaintextTargets warns for each plaintext gRPC target that is not
// loopback: the bearer token rides on every call to it.
func warnPlaintextTargets(targets map[string]string, diags *diag.Diagnostics) {
	for _, attr := range []string{endpointAttr, guardEndpointAttr} {
		target := targets[attr]
		if target == "" {
			continue
		}
		if host := targetHost(target); !isLoopback(host) {
			diags.AddAttributeWarning(path.Root("insecure"), "the bearer token travels unencrypted",
				fmt.Sprintf("insecure = true dials %s (%s) over plaintext gRPC, and every call carries the provider's bearer token, "+
					"so anyone on the path to %s can read it and use it. Use it only on a network you trust, such as a cluster's own service address; "+
					"remove insecure to use TLS.", attr, target, host))
		}
	}
}

// checkTokenURL refuses a token_url that is not https, except plain http
// with insecure, which it warns about for a host that is not loopback.
func checkTokenURL(insecure bool, tokenURL string, diags *diag.Diagnostics) {
	if tokenURL == "" {
		return
	}
	u, err := url.Parse(tokenURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		diags.AddAttributeError(path.Root("token_url"), "token_url is not an https URL",
			fmt.Sprintf("token_url %q must be an absolute https:// URL.", tokenURL))
		return
	}
	switch {
	case u.Scheme == "https":
	case !insecure:
		diags.AddAttributeError(path.Root("token_url"), "token_url is plain http",
			fmt.Sprintf("token_url %q would send the client secret unencrypted. Use https://, or, for development on a network you trust, set insecure = true.", tokenURL))
	case !isLoopback(u.Hostname()):
		diags.AddAttributeWarning(path.Root("token_url"), "the client secret travels unencrypted",
			fmt.Sprintf("token_url %q is plain http: the client secret is sent to %s in the clear, and the token it returns comes back the same way. "+
				"Use https:// outside a network you trust.", tokenURL, u.Hostname()))
	}
}

// targetHost is the host of a gRPC target: host:port, or dns:///host:port.
func targetHost(target string) string {
	t := strings.TrimPrefix(target, "dns:///")
	if host, _, err := net.SplitHostPort(t); err == nil {
		return host
	}
	return t
}

// isLoopback reports a host that never leaves the machine.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
