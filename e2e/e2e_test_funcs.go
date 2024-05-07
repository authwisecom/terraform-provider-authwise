package e2e

import (
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"testing"
)

type TestFunc interface {
	resourceName() string
	resource() string
	check(t *testing.T) resource.TestCheckFunc
}

type TestSimpleClientFunc struct {
	audienceIdentifier string
	name               string
	grantType          string
	config             string
}

func (r *TestSimpleClientFunc) resourceName() string {
	return "authwise_client.default"
}

func (r *TestSimpleClientFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "grant_type", r.grantType),
		testAccJSONConfig(t, r.resourceName(), "config", r.config),
	)
}

func (r *TestSimpleClientFunc) resource() string {
	valueMap := map[string]string{
		"name":       r.name,
		"grant_type": r.grantType,
	}
	identifierMap := map[string]string{
		"audience_id": r.audienceIdentifier,
	}
	configMap := map[string]string{
		"config": r.config,
	}

	val, err := resourceBuilder("authwise_client", "default", valueMap, identifierMap, configMap)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleRealmFunc struct {
	name             string
	userDatabaseType string
}

func (r *TestSimpleRealmFunc) resourceName() string {
	return "authwise_realm.default"
}

func (r *TestSimpleRealmFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "user_database_type", r.userDatabaseType),
	)
}

func (r *TestSimpleRealmFunc) resource() string {
	valueMap := map[string]string{
		"name":               r.name,
		"user_database_type": r.userDatabaseType,
	}

	val, err := resourceBuilder("authwise_realm", "default", valueMap, nil, nil)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleProviderFunc struct {
	realmIdentifier string
	name            string
	providerType    string
	config          string
}

func (r *TestSimpleProviderFunc) resourceName() string {
	return "authwise_provider.default"
}

func (r *TestSimpleProviderFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "provider_type", r.providerType),
		testAccJSONConfig(t, r.resourceName(), "config", r.config),
	)
}

func (r *TestSimpleProviderFunc) resource() string {
	valueMap := map[string]string{
		"name":          r.name,
		"provider_type": r.providerType,
	}
	identifierMap := map[string]string{
		"realm_id": r.realmIdentifier,
	}
	configMap := map[string]string{
		"config": r.config,
	}

	val, err := resourceBuilder("authwise_provider", "default", valueMap, identifierMap, configMap)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleAssetFunc struct {
	name     string
	mimeType string
}

func (r *TestSimpleAssetFunc) resourceName() string {
	return "authwise_asset.default"
}

func (r *TestSimpleAssetFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "mime_type", r.mimeType),
	)
}

func (r *TestSimpleAssetFunc) resource() string {
	valueMap := map[string]string{
		"name":      r.name,
		"mime_type": r.mimeType,
	}

	val, err := resourceBuilder("authwise_asset", "default", valueMap, nil, nil)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleAudienceFunc struct {
	name                        string
	appearanceProfileIdentifier string
	description                 string
	config                      string
}

func (r *TestSimpleAudienceFunc) resourceName() string {
	return "authwise_audience.default"
}

func (r *TestSimpleAudienceFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "description", r.description),
		testAccJSONConfig(t, r.resourceName(), "config", r.config),
	)
}

func (r *TestSimpleAudienceFunc) resource() string {
	valueMap := map[string]string{
		"name":        r.name,
		"description": r.description,
	}
	identifierMap := map[string]string{
		"appearance_profile_id": r.appearanceProfileIdentifier,
	}
	configMap := map[string]string{
		"config": r.config,
	}

	val, err := resourceBuilder("authwise_audience", "default", valueMap, identifierMap, configMap)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimplePermissionFunc struct {
	audienceIdentifier string
	name               string
}

func (r *TestSimplePermissionFunc) resourceName() string {
	return "authwise_permission.default"
}

func (r *TestSimplePermissionFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
	)
}

func (r *TestSimplePermissionFunc) resource() string {
	valueMap := map[string]string{
		"name": r.name,
	}
	identifierMap := map[string]string{
		"audience_id": r.audienceIdentifier,
	}

	val, err := resourceBuilder("authwise_permission", "default", valueMap, identifierMap, nil)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleRoleFunc struct {
	name               string
	auto               string
	audienceIdentifier string
}

func (r *TestSimpleRoleFunc) resourceName() string {
	return "authwise_role.default"
}

func (r *TestSimpleRoleFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "auto", r.auto),
	)
}

func (r *TestSimpleRoleFunc) resource() string {
	valueMap := map[string]string{
		"name": r.name,
		"auto": r.auto,
	}
	identifierMap := map[string]string{
		"audience_id": r.audienceIdentifier,
	}

	val, err := resourceBuilder("authwise_role", "default", valueMap, identifierMap, nil)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleScopeFunc struct {
	audienceIdentifier string
	kind               string
	auto               string
	id                 string
}

func (r *TestSimpleScopeFunc) resourceName() string {
	return "authwise_scope.default"
}

func (r *TestSimpleScopeFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "id", r.id),
		resource.TestCheckResourceAttr(r.resourceName(), "kind", r.kind),
		resource.TestCheckResourceAttr(r.resourceName(), "auto", r.auto),
	)
}

func (r *TestSimpleScopeFunc) resource() string {
	valueMap := map[string]string{
		"id":   r.id,
		"kind": r.kind,
		"auto": r.auto,
	}
	identifierMap := map[string]string{
		"audience_id": r.audienceIdentifier,
	}

	val, err := resourceBuilder("authwise_scope", "default", valueMap, identifierMap, nil)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleSecretFunc struct {
	name     string
	encoding string
	value    string
}

func (r *TestSimpleSecretFunc) resourceName() string {
	return "authwise_secret.default"
}

func (r *TestSimpleSecretFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "encoding", r.encoding),
		resource.TestCheckResourceAttr(r.resourceName(), "value", r.value),
	)
}

func (r *TestSimpleSecretFunc) resource() string {
	valueMap := map[string]string{
		"name":     r.name,
		"encoding": r.encoding,
		"value":    r.value,
	}

	val, err := resourceBuilder("authwise_secret", "default", valueMap, nil, nil)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleTenantFunc struct {
	name                        string
	appearanceProfileIdentifier string
	config                      string
}

func (r *TestSimpleTenantFunc) resourceName() string {
	return "authwise_tenant.default"
}

func (r *TestSimpleTenantFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		testAccJSONConfig(t, r.resourceName(), "config", r.config),
	)
}

func (r *TestSimpleTenantFunc) resource() string {
	valueMap := map[string]string{
		"name":                  r.name,
		"appearance_profile_id": r.appearanceProfileIdentifier,
	}
	identifierMap := map[string]string{
		"appearance_profile_id": r.appearanceProfileIdentifier,
	}
	configMap := map[string]string{
		"config": r.config,
	}

	val, err := resourceBuilder("authwise_tenant", "default", valueMap, identifierMap, configMap)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleThemeFunc struct {
	name                 string
	stylesheet           string
	stylesheetAttributes string
	content              string
}

func (r *TestSimpleThemeFunc) resourceName() string {
	return "authwise_theme.default"
}

func (r *TestSimpleThemeFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		resource.TestCheckResourceAttr(r.resourceName(), "stylesheet", r.stylesheet),
		testAccJSONConfig(t, r.resourceName(), "stylesheet_attributes", r.stylesheetAttributes),
		testAccJSONConfig(t, r.resourceName(), "content", r.content),
	)
}

func (r *TestSimpleThemeFunc) resource() string {

	valueMap := map[string]string{
		"name":       r.name,
		"stylesheet": r.stylesheet,
	}
	configMap := map[string]string{
		"stylesheet_attributes": r.stylesheetAttributes,
		"content":               r.content,
	}

	val, err := resourceBuilder("authwise_theme", "default", valueMap, nil, configMap)
	if err != nil {
		panic("error building resource")
	}

	return val
}

type TestSimpleAppearanceProfileFunc struct {
	name                 string
	themeIdentifier      string
	stylesheetAttributes string
	content              string
}

func (r *TestSimpleAppearanceProfileFunc) resourceName() string {
	return "authwise_appearance_profile.default"
}

func (r *TestSimpleAppearanceProfileFunc) check(t *testing.T) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(r.resourceName(), "name", r.name),
		testAccJSONConfig(t, r.resourceName(), "stylesheet_attributes", r.stylesheetAttributes),
		testAccJSONConfig(t, r.resourceName(), "content", r.content),
	)
}

func (r *TestSimpleAppearanceProfileFunc) resource() string {
	valueMap := map[string]string{
		"name": r.name,
	}
	identifierMap := map[string]string{
		"theme_id": r.themeIdentifier,
	}
	configMap := map[string]string{
		"stylesheet_attributes": r.stylesheetAttributes,
		"content":               r.content,
	}

	val, err := resourceBuilder("authwise_appearance_profile", "default", valueMap, identifierMap, configMap)
	if err != nil {
		panic("error building resource")
	}

	return val
}
