package provider

import "strings"

// The Registry groups a provider's pages by the subcategory in each page's
// frontmatter, and by nothing else (#31). Five groups: the two typed-variant
// families get their own headings, or Identity would hold most of the
// provider and be the flat list again.
const (
	SubcategoryIdentity  = "Identity"
	SubcategoryProviders = "Sign-in providers"
	SubcategoryFactors   = "Factors"
	SubcategoryAccess    = "Access"
	SubcategoryGuard     = "Guard"
)

// identityTypes are the Identity group's resource and data source types,
// without the provider prefix. A plural data source takes its singular's
// group. scope_access_permissions is the scope's edge, not an Access
// object.
var identityTypes = map[string]bool{
	"domain": true, "issuer": true, "realm": true, "audience": true,
	"scope": true, "scope_access_permissions": true, "theme": true,
	"appearance_profile": true, "asset": true, "asset_content": true,
	"secret": true, "certificate": true, "endpoint": true, "client": true,
	"client_secret": true, "interactive_client_config": true,
	"saml_relying_party_config": true, "user": true,
	"realm_authentication_policy": true, "realm_authentication_context_schema": true,
}

// families are the groups that go by prefix, so a new Guard, Access,
// provider or factor type cannot land outside its group. A family's root
// type and its plural ("provider", "providers") belong to it too.
var families = []struct {
	root, group string
}{
	{"guard", SubcategoryGuard},
	{"access", SubcategoryAccess},
	{"provider", SubcategoryProviders},
	{"factor", SubcategoryFactors},
}

// Subcategory is the Registry group of a resource or data source type
// ("authwise_guard_network"), or "" when it has none. Anything outside the
// families must be listed in identityTypes.
func Subcategory(typeName string) string {

	name, ok := strings.CutPrefix(typeName, providerTypeName+"_")
	if !ok {
		return ""
	}

	for _, f := range families {
		if name == f.root || name == f.root+"s" || strings.HasPrefix(name, f.root+"_") {
			return f.group
		}
	}

	if identityTypes[name] || identityTypes[strings.TrimSuffix(name, "s")] {
		return SubcategoryIdentity
	}

	return ""
}
