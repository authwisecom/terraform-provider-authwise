package acctest_test

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Domain, Scope and AppearanceProfile: the three resources whose keying and
// collection the provider took from kit's proto http annotations without
// exercising them (#15). The fakes enforce the same routes — the parent
// pattern and, for the two caller-named ones, the id carried in the
// entity's name field — so a provider that composed either wrongly fails
// here rather than against a real kit.

var (
	tenantParent   = regexp.MustCompile(`^tenants/[^/]+$`)
	issuerParent   = regexp.MustCompile(`^tenants/[^/]+/issuers/[^/]+$`)
	audienceParent = regexp.MustCompile(`^tenants/[^/]+/issuers/[^/]+/audiences/[^/]+$`)
)

func checkParent(kind string, re *regexp.Regexp, parent string) error {
	if !re.MatchString(parent) {
		return status.Errorf(codes.InvalidArgument, "%s parent %q does not match %s", kind, parent, re)
	}
	return nil
}

// --- Domain (tenant-scoped, keyed by an AWID since kit#808; the hostname is
// unique install-wide). A custom host is created with its records and stays
// PENDING_DNS: its lifecycle is the issuer-domains fake's, and the
// provider's unit tests'. ---

func (f *fakeIdentityServer) GetDomain(ctx context.Context, in *identitypb.GetDomainRequest) (*corepb.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	d, ok := f.domains[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "domain %q not found", in.GetName())
	}
	out := proto.Clone(d).(*corepb.Domain)
	// Who answers here (kit#911), filled on Get only.
	for _, name := range f.bindingOrder {
		if b := f.bindings[name]; tenantOf(name)+"/domains/"+b.GetDomainId() == in.GetName() {
			out.Bindings = append(out.Bindings, proto.Clone(b).(*corepb.IssuerDomainBinding))
		}
	}
	return out, nil
}

func (f *fakeIdentityServer) ListDomains(ctx context.Context, in *identitypb.ListDomainsRequest) (*identitypb.ListDomainsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	out := &identitypb.ListDomainsResponse{}
	for _, name := range slices.Sorted(maps.Keys(f.domains)) {
		if strings.HasPrefix(name, in.GetParent()+"/domains/") {
			out.Domains = append(out.Domains, proto.Clone(f.domains[name]).(*corepb.Domain))
		}
	}
	return out, nil
}

func (f *fakeIdentityServer) CreateDomain(ctx context.Context, in *identitypb.CreateDomainRequest) (*corepb.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("domain", tenantParent, in.GetParent()); err != nil {
		return nil, err
	}
	hostname := in.GetDomain().GetHostname()
	if hostname == "" {
		return nil, status.Error(codes.InvalidArgument, "hostname is required")
	}
	// A custom domain stays PENDING_DNS: the fake has no DNS to verify.
	// issuer_id is a deprecated create convenience (kit#911): it claims
	// the host and binds that issuer at its root.
	issuerID := in.GetDomain().GetIssuerId()
	if issuerID != "" {
		if _, ok := f.issuers[in.GetParent()+"/issuers/"+issuerID]; !ok {
			return nil, status.Errorf(codes.InvalidArgument, "issuer_id: no issuer %q in %s", issuerID, in.GetParent())
		}
	}
	// purpose (kit#942) replaced product (kit#903), which is refused.
	// A Guard or console address is a custom host, never beside an issuer.
	if in.GetDomain().GetProduct() != "" {
		return nil, refusal(codes.InvalidArgument, "DOMAIN_PRODUCT_RETIRED", nil, "product is retired: set purpose")
	}
	purpose := in.GetDomain().GetPurpose()
	switch {
	case purpose != "" && purpose != "issuer" && purpose != "guard" && purpose != "console":
		return nil, status.Errorf(codes.InvalidArgument, "purpose: %q is not issuer, guard or console", purpose)
	case (purpose == "guard" || purpose == "console") && issuerID != "":
		return nil, status.Error(codes.InvalidArgument, "a Guard or console address serves no issuer: issuer_id is not allowed")
	case purpose == "console" && !in.GetDomain().GetCustom():
		return nil, refusal(codes.InvalidArgument, "CONSOLE_DOMAIN_NOT_CUSTOM", nil, "a console domain is a custom host")
	}
	for _, d := range f.domains {
		if d.GetHostname() == hostname {
			return nil, status.Errorf(codes.AlreadyExists, "hostname %q is claimed", hostname)
		}
	}
	d := proto.Clone(in.GetDomain()).(*corepb.Domain)
	d.Name = in.GetParent() + "/domains/" + f.nextID("d")
	d.IssuerId, d.Bindings = "", nil
	if d.GetPurpose() == "" {
		d.Purpose = "issuer"
	}
	// For one release kit also reads product out on a Guard or console row.
	if d.GetPurpose() != "issuer" {
		d.Product = d.GetPurpose()
	}
	d.Custom = d.GetCustom() || issuerID != "" || d.GetPurpose() != "issuer"
	if d.GetCustom() {
		// The traffic CNAME targets the install's edge name (kit#911,
		// D5), for a console address too (kit#942); kit leaves a Guard
		// address's empty, and its record points at the tenant's default
		// Guard host.
		target := "acme.guard.authwise.test"
		switch d.GetPurpose() {
		case "issuer":
			d.TrafficTarget = "edge.authwise.test"
			target = d.GetTrafficTarget()
		case "console":
			d.TrafficTarget = "edge.authwise.test"
			target = d.GetTrafficTarget()
		}
		d.State = corepb.IssuerDomainState_ISSUER_DOMAIN_STATE_PENDING_DNS
		d.DnsRecords = []*corepb.IssuerDomainDnsRecord{
			{Name: hostname, Type: "CNAME", Value: target, Purpose: "traffic"},
			{Name: "_acme-challenge." + hostname, Type: "CNAME", Value: "abc.authorize.certificatemanager.goog", Purpose: "challenge"},
		}
	}
	f.domains[d.GetName()] = d
	if issuerID != "" {
		id := d.GetName()[strings.LastIndex(d.GetName(), "/")+1:]
		if err := f.admitBindingLocked(in.GetParent()+"/issuers/"+issuerID, id, ""); err != nil {
			delete(f.domains, d.GetName())
			return nil, err
		}
		f.bindLocked(in.GetParent()+"/issuers/"+issuerID, id, "")
	}
	return proto.Clone(d).(*corepb.Domain), nil
}

func (f *fakeIdentityServer) PatchDomain(ctx context.Context, in *identitypb.PatchDomainRequest) (*corepb.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.domains[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "domain %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "labels":
			existing.Labels = in.GetDomain().GetLabels()
		case "config":
			existing.Config = in.GetDomain().GetConfig()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Domain), nil
}

func (f *fakeIdentityServer) DeleteDomain(ctx context.Context, in *identitypb.DeleteDomainRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.domains[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "domain %q not found", in.GetName())
	}
	// DOMAIN_IN_USE (kit#911): refused while bound.
	var bound []string
	for _, name := range f.bindingOrder {
		if b := f.bindings[name]; tenantOf(name)+"/domains/"+b.GetDomainId() == in.GetName() {
			bound = append(bound, b.GetIssuerId())
		}
	}
	if len(bound) > 0 {
		return nil, refusal(codes.FailedPrecondition, "DOMAIN_IN_USE", map[string]string{"issuers": strings.Join(bound, ",")},
			"domain %q is bound to issuers %s", in.GetName(), strings.Join(bound, ", "))
	}
	delete(f.domains, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Scope (audience-scoped, caller-named: the id is the scope string) ---

func (f *fakeIdentityServer) GetScope(ctx context.Context, in *identitypb.GetScopeRequest) (*corepb.Scope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	s, ok := f.scopes[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "scope %q not found", in.GetName())
	}
	return proto.Clone(s).(*corepb.Scope), nil
}

func (f *fakeIdentityServer) CreateScope(ctx context.Context, in *identitypb.CreateScopeRequest) (*corepb.Scope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("scope", audienceParent, in.GetParent()); err != nil {
		return nil, err
	}
	name, err := callerName(in.GetParent(), "scopes", in.GetScope().GetName())
	if err != nil {
		return nil, err
	}
	if _, exists := f.scopes[name]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "scope %q already exists", name)
	}
	s := proto.Clone(in.GetScope()).(*corepb.Scope)
	s.Name = name
	f.scopes[name] = s
	return proto.Clone(s).(*corepb.Scope), nil
}

func (f *fakeIdentityServer) PatchScope(ctx context.Context, in *identitypb.PatchScopeRequest) (*corepb.Scope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.scopes[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "scope %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "kind":
			existing.Kind = in.GetScope().GetKind()
		case "auto":
			existing.Auto = in.GetScope().GetAuto()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Scope), nil
}

func (f *fakeIdentityServer) DeleteScope(ctx context.Context, in *identitypb.DeleteScopeRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.scopes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "scope %q not found", in.GetName())
	}
	delete(f.scopes, in.GetName())
	delete(f.scopePerms, in.GetName())
	return &emptypb.Empty{}, nil
}

func (f *fakeIdentityServer) AssociateAccessPermissionsToScope(ctx context.Context, in *identitypb.AssociateAccessPermissionsToScopeRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.scopes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "scope %q not found", in.GetName())
	}
	if f.scopePerms[in.GetName()] == nil {
		f.scopePerms[in.GetName()] = map[string]bool{}
	}
	for _, n := range in.GetAssociation().GetSet() {
		f.scopePerms[in.GetName()][n] = true
	}
	for _, n := range in.GetAssociation().GetRemove() {
		delete(f.scopePerms[in.GetName()], n)
	}
	return &emptypb.Empty{}, nil
}

// ListAccessPermissionsByScope pages one at a time, like the role listing,
// so the association resource's refresh walks the page tokens.
func (f *fakeIdentityServer) ListAccessPermissionsByScope(ctx context.Context, in *identitypb.ListAccessPermissionsByScopeRequest) (*identitypb.ListAccessPermissionsByScopeResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.scopes[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "scope %q not found", in.GetName())
	}
	names := f.scopePermissionsLocked(in.GetName())
	start := 0
	if in.GetPageToken() != "" {
		fmt.Sscanf(in.GetPageToken(), "%d", &start)
	}
	res := &identitypb.ListAccessPermissionsByScopeResponse{}
	if start < len(names) {
		res.AccessPermissions = []*corepb.AccessPermission{{Name: names[start]}}
		if start+1 < len(names) {
			res.NextPageToken = fmt.Sprintf("%d", start+1)
		}
	}
	return res, nil
}

// scopePermissionsLocked returns a scope's access permissions, sorted. The
// caller holds f.mu.
func (f *fakeIdentityServer) scopePermissionsLocked(scope string) []string {
	out := make([]string, 0, len(f.scopePerms[scope]))
	for n := range f.scopePerms[scope] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// --- AppearanceProfile (issuer-scoped, collection "appearance-profiles") ---
//
// kit holds exactly one default per issuer once it has a profile (kit#680):
// the first profile created is the default whatever is_default says; a write
// that would make a second default, or a mask that clears the default's
// flag, is FAILED_PRECONDITION; the default cannot be deleted while others
// remain; and MakeDefaultAppearanceProfile moves it.

// issuerOfProfile is the issuer a profile's name sits under.
func issuerOfProfile(name string) string {
	issuer, _, _ := strings.Cut(name, "/appearance-profiles/")
	return issuer
}

// defaultProfileLocked is the issuer's default profile, or nil.
func (f *fakeIdentityServer) defaultProfileLocked(issuer string) *corepb.AppearanceProfile {
	for name, p := range f.profiles {
		if issuerOfProfile(name) == issuer && p.GetIsDefault() {
			return p
		}
	}
	return nil
}

func (f *fakeIdentityServer) GetAppearanceProfile(ctx context.Context, in *identitypb.GetAppearanceProfileRequest) (*corepb.AppearanceProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	p, ok := f.profiles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "appearance profile %q not found", in.GetName())
	}
	return proto.Clone(p).(*corepb.AppearanceProfile), nil
}

func (f *fakeIdentityServer) CreateAppearanceProfile(ctx context.Context, in *identitypb.CreateAppearanceProfileRequest) (*corepb.AppearanceProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("appearance profile", issuerParent, in.GetParent()); err != nil {
		return nil, err
	}
	p := proto.Clone(in.GetAppearanceProfile()).(*corepb.AppearanceProfile)
	current := f.defaultProfileLocked(in.GetParent())
	if p.GetIsDefault() && current != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "the issuer's default is %s; make this the default with :makeDefault", current.GetName())
	}
	p.IsDefault = current == nil
	p.Name = in.GetParent() + "/appearance-profiles/" + f.nextID("ap")
	f.profiles[p.GetName()] = p
	return proto.Clone(p).(*corepb.AppearanceProfile), nil
}

func (f *fakeIdentityServer) PatchAppearanceProfile(ctx context.Context, in *identitypb.PatchAppearanceProfileRequest) (*corepb.AppearanceProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.profiles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "appearance profile %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "labels":
			existing.Labels = in.GetAppearanceProfile().GetLabels()
		case "display_name":
			existing.DisplayName = in.GetAppearanceProfile().GetDisplayName()
		case "theme_id":
			existing.ThemeId = in.GetAppearanceProfile().GetThemeId()
		case "stylesheet_attributes":
			existing.StylesheetAttributes = in.GetAppearanceProfile().GetStylesheetAttributes()
		case "content":
			existing.Content = in.GetAppearanceProfile().GetContent()
		case "is_default":
			if in.GetAppearanceProfile().GetIsDefault() != existing.GetIsDefault() {
				return nil, status.Errorf(codes.FailedPrecondition, "is_default moves only through :makeDefault")
			}
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.AppearanceProfile), nil
}

func (f *fakeIdentityServer) MakeDefaultAppearanceProfile(ctx context.Context, in *identitypb.MakeDefaultAppearanceProfileRequest) (*identitypb.MakeDefaultAppearanceProfileResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	f.makeDefaults++
	p, ok := f.profiles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "appearance profile %q not found", in.GetName())
	}
	out := &identitypb.MakeDefaultAppearanceProfileResponse{}
	if previous := f.defaultProfileLocked(issuerOfProfile(in.GetName())); previous != nil && previous != p {
		previous.IsDefault = false
		out.Previous = proto.Clone(previous).(*corepb.AppearanceProfile)
	}
	p.IsDefault = true
	out.Default = proto.Clone(p).(*corepb.AppearanceProfile)
	return out, nil
}

func (f *fakeIdentityServer) DeleteAppearanceProfile(ctx context.Context, in *identitypb.DeleteAppearanceProfileRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	p, ok := f.profiles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "appearance profile %q not found", in.GetName())
	}
	if p.GetIsDefault() {
		issuer := issuerOfProfile(in.GetName())
		for name := range f.profiles {
			if name != in.GetName() && issuerOfProfile(name) == issuer {
				return nil, status.Errorf(codes.FailedPrecondition, "%s is the issuer's default and the issuer has other profiles", in.GetName())
			}
		}
	}
	delete(f.profiles, in.GetName())
	return &emptypb.Empty{}, nil
}
