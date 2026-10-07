package acctest_test

import (
	"slices"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// What a client may do, the kit#404 shape (apis v0.21.0, kit v1.38.0):
// grant_types, kind, token_endpoint_auth_method, status and expires_at. The
// rules are kit's client_facts.go setClientFactDefaults and
// validateClientFacts, applied on create and on every patch.

const (
	authMethodNone = "none"
	grantAuthCode  = "authorization_code"
	grantRefresh   = "refresh_token"
	grantClientCC  = "client_credentials"
	grantSAMLIDP   = "saml_idp"
)

var clientGrantTypes = []string{grantAuthCode, grantRefresh, grantClientCC, grantSAMLIDP}

// setClientDefaults fills what a client may leave unset: its kind from its
// grants, its method from its kind, and active.
func setClientDefaults(c *corepb.Client) {
	if c.GetKind() == corepb.ClientKind_CLIENT_KIND_UNSPECIFIED && len(c.GetGrantTypes()) > 0 {
		c.Kind = corepb.ClientKind_CLIENT_KIND_APPLICATION
		if len(c.GetGrantTypes()) == 1 && c.GetGrantTypes()[0] == grantClientCC {
			c.Kind = corepb.ClientKind_CLIENT_KIND_SERVICE
		}
	}
	if c.GetTokenEndpointAuthMethod() == "" && c.GetKind() != corepb.ClientKind_CLIENT_KIND_UNSPECIFIED {
		c.TokenEndpointAuthMethod = "client_secret_basic"
		if c.GetKind() == corepb.ClientKind_CLIENT_KIND_APPLICATION {
			c.TokenEndpointAuthMethod = authMethodNone
		}
	}
	if c.GetStatus() == corepb.ClientStatus_CLIENT_STATUS_UNSPECIFIED {
		c.Status = corepb.ClientStatus_CLIENT_STATUS_ACTIVE
	}
}

// admitClient fills a client's defaults and refuses what kit refuses.
func admitClient(c *corepb.Client) error {

	setClientDefaults(c)

	seen := map[string]bool{}
	for _, g := range c.GetGrantTypes() {
		if !slices.Contains(clientGrantTypes, g) {
			return status.Errorf(codes.InvalidArgument, "grant_types: %q is not a grant a client may be registered for (one of %v)", g, clientGrantTypes)
		}
		if seen[g] {
			return status.Errorf(codes.InvalidArgument, "grant_types: %q is listed twice", g)
		}
		seen[g] = true
	}
	if seen[grantRefresh] && !seen[grantAuthCode] {
		return status.Error(codes.InvalidArgument, "grant_types: refresh_token is only for a client that also has authorization_code")
	}
	if seen[grantSAMLIDP] && len(c.GetGrantTypes()) > 1 {
		return status.Error(codes.InvalidArgument, "grant_types: a saml_idp client is a SAML relying party and has no OAuth grants")
	}

	if c.GetKind() == corepb.ClientKind_CLIENT_KIND_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, `kind: "" is not application, service or agent`)
	}

	switch c.GetTokenEndpointAuthMethod() {
	case "client_secret_basic", "client_secret_post":
	case authMethodNone:
		if c.GetKind() != corepb.ClientKind_CLIENT_KIND_APPLICATION {
			return status.Errorf(codes.InvalidArgument, "token_endpoint_auth_method: none is for an application; a %s authenticates", c.GetKind())
		}
		if seen[grantClientCC] {
			return status.Error(codes.InvalidArgument, "token_endpoint_auth_method: a public client cannot use client_credentials, which is for a client that authenticates")
		}
	case "private_key_jwt":
		return status.Error(codes.InvalidArgument, "token_endpoint_auth_method: private_key_jwt is not supported yet (kit#411)")
	default:
		return status.Errorf(codes.InvalidArgument, "token_endpoint_auth_method: %q is not client_secret_basic, client_secret_post or none", c.GetTokenEndpointAuthMethod())
	}

	return nil
}
