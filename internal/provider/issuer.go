package provider

import (
	"context"
	"regexp"

	"git.authwise.com/authwise/terraform-provider-authwise/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// issuerTypeName is the Terraform type of the generated issuer resource,
// wrapped to say what kit#911 made of its domain fields.
const issuerTypeName = "authwise_issuer"

// issuerPathPattern is kit's canonical issuer path (kit#875): "" or
// segments, each "/" and one or more characters other than "/", "?", "#"
// or white space, with no trailing "/". kit stores it as written; the
// reserved first segments (kit#876) are kit's to refuse, since that list
// moves with kit's routes.
var issuerPathPattern = regexp.MustCompile(`^(/[^/?#\s]+)*$`)

// withIssuerPathForm adds the canonical-form check to a path attribute.
func withIssuerPathForm(attrs map[string]schema.Attribute, diags *diag.Diagnostics) {
	a, ok := attrs["path"].(schema.StringAttribute)
	if !ok {
		diags.AddError("schema", "no string path attribute")
		return
	}
	a.Validators = append(a.Validators, stringvalidator.RegexMatches(issuerPathPattern,
		`must be "" for the domain's root, or segments like "/app" or "/a/b": each starts with "/", none is empty, no trailing "/", no "?" or "#"`))
	attrs["path"] = a
}

// issuerBindingDeprecation is the domain fields' deprecation (tf#69).
const issuerBindingDeprecation = "kit#911: an issuer answers at a host through authwise_issuer_domain_binding. " +
	"This is a create-time convenience that makes the first binding, read back afterwards; kit removes it at the contract (kit#912)."

var (
	_ resource.Resource                = &issuerResource{}
	_ resource.ResourceWithConfigure   = &issuerResource{}
	_ resource.ResourceWithImportState = &issuerResource{}
)

// issuerResource is the generated authwise_issuer with the descriptions
// and deprecations of its domain fields. domain_id and path keep the
// values the issuer was created with: kit reads them from the first
// binding, which moves when that binding goes, and a moved value would
// plan a replacement of the issuer.
type issuerResource struct {
	inner resource.Resource
}

func newIssuerResource() resource.Resource {
	return &issuerResource{inner: generated.NewIssuerResource()}
}

func (r *issuerResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *issuerResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {

	r.inner.Schema(ctx, req, resp)

	describe := func(name, desc, deprecation string) {
		a, ok := resp.Schema.Attributes[name].(schema.StringAttribute)
		if !ok {
			resp.Diagnostics.AddError("schema", "the generated issuer has no string "+name)
			return
		}
		a.MarkdownDescription, a.DeprecationMessage = desc, deprecation
		resp.Schema.Attributes[name] = a
	}

	describe(domainIDAttribute, "**Deprecated** (kit#911): use `authwise_issuer_domain_binding`. On create, the domain (`d-…`) the issuer's first binding is made on, at `path`; "+
		"optional, since an issuer may exist with no binding. It keeps the value it was created with; `bindings` shows where the issuer answers now. Changing a configured value replaces the issuer.", issuerBindingDeprecation)
	describe("path", "**Deprecated** (kit#911): use `authwise_issuer_domain_binding`. On create, the first binding's path: `\"\"` (or unset) for the domain's root, else a path such as `/customers` (see `authwise_issuer_domain_binding.path`). "+
		"It keeps the value it was created with. Changing a configured value replaces the issuer.", issuerBindingDeprecation)
	withIssuerPathForm(resp.Schema.Attributes, &resp.Diagnostics)
	describe("domain_name", "The hostname of the issuer's first binding; empty when it has none.", "")
	describe("preferred_binding_id", "The binding (`idb-…`) kit builds the links it mails on: the invitation accept, the password reset, the address verification. "+
		"Empty means the issuer's first binding. Written by `authwise_issuer_preferred_binding`, since a binding names its issuer and the issuer naming the binding back would be a cycle.", "")
	describe("preferred_domain_id", "**Deprecated** (kit#911) for `preferred_binding_id`: the preferred binding's domain.", "")
}

func (r *issuerResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *issuerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.inner.Create(ctx, req, resp)
	keepStrings(ctx, req.Plan, &resp.State, &resp.Diagnostics, domainIDAttribute, "path")
}

func (r *issuerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
	keepStrings(ctx, req.State, &resp.State, &resp.Diagnostics, domainIDAttribute, "path")
}

func (r *issuerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.inner.Update(ctx, req, resp)
	keepStrings(ctx, req.Plan, &resp.State, &resp.Diagnostics, domainIDAttribute, "path")
}

func (r *issuerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *issuerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}

// issuerDomainBindingTypeName is the generated binding resource, wrapped
// to keep `path = ""` as written.
const issuerDomainBindingTypeName = "authwise_issuer_domain_binding"

// issuerDomainBindingResource is the generated authwise_issuer_domain_binding
// with an explicit root path kept.
type issuerDomainBindingResource struct {
	issuerResource
}

func newIssuerDomainBindingResource() resource.Resource {
	return &issuerDomainBindingResource{issuerResource{inner: generated.NewIssuerDomainBindingResource()}}
}

func (r *issuerDomainBindingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {

	r.inner.Schema(ctx, req, resp)

	set := func(name, desc string) {
		switch a := resp.Schema.Attributes[name].(type) {
		case schema.StringAttribute:
			a.MarkdownDescription = desc
			resp.Schema.Attributes[name] = a
		case schema.BoolAttribute:
			a.MarkdownDescription = desc
			resp.Schema.Attributes[name] = a
		default:
			resp.Diagnostics.AddError("schema", "the generated binding has no "+name)
		}
	}
	set(issuerIDAttribute, "The issuer (`i-…`), `authwise_issuer.<name>.issuer_id`; overrides the provider default. Changing it replaces the binding.")
	set(domainIDAttribute, "The domain (`d-…`) of the issuer's tenant, `authwise_domain.<name>.domain_id`: native, or custom. Never a product's host (`DOMAIN_NOT_BINDABLE`). Changing it replaces the binding.")
	set("path", "`\"\"` (or unset) for the domain's root, else one or more segments such as `/customers` or `/a/b`: each starts with `/`, none is empty, no trailing `/`, no `?` or `#`. "+
		"kit stores it exactly as written; `\"/\"` is not the root and is refused. A path whose first segment kit serves on every host is reserved (`issuer path reserved`, kit#876): "+
		"`/.well-known`, `/authorize`, `/oauth`, `/userinfo`, `/api`, `/_ui`, `/assets`, `/account`, `/login`, `/logout`, `/recover-password`, `/health`, `/saml`, `/recovery`, `/magic-link`, `/forgot-password`, `/uiassets` and `/admin`. One (domain, path) serves one issuer: another issuer there is refused (`BINDING_TAKEN`). Changing it replaces the binding.")
	withIssuerPathForm(resp.Schema.Attributes, &resp.Diagnostics)
	set("hostname", "The domain's hostname.")
	set("url", "The binding's issuer URL (`iss`): `https://<hostname><path>`.")
	set("routable", "Whether kit answers here now: always on a native domain; on a custom one once its certificate is live (`PROVISIONING`, `ACTIVE` or `DEGRADED`). A binding may be made before then.")
}
