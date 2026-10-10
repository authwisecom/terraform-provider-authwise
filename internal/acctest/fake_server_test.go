package acctest_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	accesspb "git.authwise.com/authwise/apis/authwise/access/v1alpha1"
	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// certNotBefore is the fixed issue instant the fake stamps on every minted
// certificate, so not_before and not_after are assertable.
var certNotBefore = time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

// fakeIdentityServer is an in-memory AIP server for the acceptance tier:
// real terraform CLI + the real provider + the real client_credentials
// exchange, with only the Authwise API replaced. It implements the
// resources the acceptance tests touch; everything else stays
// Unimplemented.
type fakeIdentityServer struct {
	identitypb.UnimplementedAuthwiseIdentityServiceServer

	mu       sync.Mutex
	realms   map[string]*corepb.Realm
	issuers  map[string]*corepb.Issuer
	domains  map[string]*corepb.Domain
	scopes   map[string]*corepb.Scope
	profiles map[string]*corepb.AppearanceProfile
	// scopePerms holds each scope's access-permission set, by name.
	scopePerms map[string]map[string]bool
	audiences  map[string]*corepb.Audience
	clients    map[string]*corepb.Client
	// clientSecrets are the minted rows. Like kit's they hold no
	// credential: the secret exists on the wire only in the mint response.
	clientSecrets map[string]*corepb.ClientSecret
	// mints counts MintClientSecret calls, so a test can tell a
	// replacement from an in-place update.
	mints int
	// makeDefaults counts MakeDefaultAppearanceProfile calls.
	makeDefaults int
	providers    map[string]*corepb.Provider
	certs        map[string]*corepb.Certificate
	endpoints    map[string]*corepb.Endpoint
	factors      map[string]*corepb.Factor
	secrets      map[string]*corepb.Secret
	assets       map[string]*corepb.Asset
	users        map[string]*corepb.User
	// contact is each user's derived address, apart from the row: kit
	// fills email from the account's proven identifiers on every read.
	contact map[string]string
	seq     int

	// blobs holds each asset's content as uploaded, apart from the row,
	// the way kit keeps it in a bucket. uploads counts completed
	// UploadAsset streams.
	blobs   map[string][]byte
	uploads int

	// material holds each secret's versions as sent, oldest first. The
	// stored Secret has no field for it, exactly as kit's does not, so this
	// is the only place a test can see what the provider sent.
	material map[string][]string

	// pinned names secrets referenced by something outside the test's
	// configuration, so deleting them is refused the way kit refuses it.
	pinned map[string]bool

	// lastMint records the input-only create parameters of the most recent
	// CreateCertificate. The stored row deliberately keeps none of them, so
	// this is the only way a test can see what the provider actually sent.
	lastMint *corepb.Certificate

	// lastAuthorization records the auth metadata of the most recent call
	// so tests can assert the bearer flow end to end.
	lastAuthorization string
}

func newFakeIdentityServer() *fakeIdentityServer {
	return &fakeIdentityServer{
		realms:        map[string]*corepb.Realm{},
		issuers:       map[string]*corepb.Issuer{},
		domains:       map[string]*corepb.Domain{},
		scopes:        map[string]*corepb.Scope{},
		profiles:      map[string]*corepb.AppearanceProfile{},
		scopePerms:    map[string]map[string]bool{},
		audiences:     map[string]*corepb.Audience{},
		clients:       map[string]*corepb.Client{},
		clientSecrets: map[string]*corepb.ClientSecret{},
		providers:     map[string]*corepb.Provider{},
		certs:         map[string]*corepb.Certificate{},
		endpoints:     map[string]*corepb.Endpoint{},
		factors:       map[string]*corepb.Factor{},
		secrets:       map[string]*corepb.Secret{},
		assets:        map[string]*corepb.Asset{},
		users:         map[string]*corepb.User{},
		contact:       map[string]string{},
		blobs:         map[string][]byte{},
		material:      map[string][]string{},
		pinned:        map[string]bool{},
	}
}

func (f *fakeIdentityServer) recordAuth(ctx context.Context) {
	if v := authorizationOf(ctx); v != "" {
		f.lastAuthorization = v
	}
}

// authorizationOf is the authorization header a call carried, or "".
func authorizationOf(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("authorization"); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func (f *fakeIdentityServer) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%08d", prefix, f.seq)
}

// --- Realm ---

func (f *fakeIdentityServer) GetRealm(ctx context.Context, in *identitypb.GetRealmRequest) (*corepb.Realm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	r, ok := f.realms[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	return proto.Clone(r).(*corepb.Realm), nil
}

func (f *fakeIdentityServer) ListRealms(ctx context.Context, in *identitypb.ListRealmsRequest) (*identitypb.ListRealmsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	page, next := pageUnder(f.realms, in.GetParent()+"/realms/", in.GetPageToken())
	return &identitypb.ListRealmsResponse{Realms: page, NextPageToken: next}, nil
}

// ListProviders pages the realm's providers two at a time.
func (f *fakeIdentityServer) ListProviders(ctx context.Context, in *identitypb.ListProvidersRequest) (*identitypb.ListProvidersResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	page, next := pageUnder(f.providers, in.GetParent()+"/providers/", in.GetPageToken())
	return &identitypb.ListProvidersResponse{Providers: page, NextPageToken: next}, nil
}

// fakePageSize is small on purpose: a list data source has to follow page
// tokens to see everything.
const fakePageSize = 2

// pageUnder returns one page of the rows whose names start with prefix, in
// name order, cloned, and the token of the next page ("" on the last).
func pageUnder[M proto.Message](rows map[string]M, prefix, token string) ([]M, string) {

	names := make([]string, 0, len(rows))
	for name := range rows {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	start, _ := strconv.Atoi(token)
	end := min(start+fakePageSize, len(names))
	page := make([]M, 0, fakePageSize)
	for _, name := range names[min(start, end):end] {
		page = append(page, proto.Clone(rows[name]).(M))
	}
	if end < len(names) {
		return page, strconv.Itoa(end)
	}
	return page, ""
}

func (f *fakeIdentityServer) CreateRealm(ctx context.Context, in *identitypb.CreateRealmRequest) (*corepb.Realm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitRealmConfigLocked(in.GetRealm()); err != nil {
		return nil, err
	}
	r := proto.Clone(in.GetRealm()).(*corepb.Realm)
	r.Name = in.GetParent() + "/realms/" + f.nextID("r")
	f.realms[r.GetName()] = r
	return proto.Clone(r).(*corepb.Realm), nil
}

func (f *fakeIdentityServer) PatchRealm(ctx context.Context, in *identitypb.PatchRealmRequest) (*corepb.Realm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.realms[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetRealm().GetDisplayName()
		case "config":
			if err := f.admitRealmConfigLocked(in.GetRealm()); err != nil {
				return nil, err
			}
			existing.Config = in.GetRealm().GetConfig()
		case "config.authentication":
			// kit merges a nested path on its own and refuses one whose
			// parent the request leaves unset (validateMaskParents).
			if in.GetRealm().GetConfig() == nil {
				return nil, status.Errorf(codes.InvalidArgument, "update_mask path %q: config is unset", path)
			}
			if existing.GetConfig() == nil {
				existing.Config = &corepb.RealmConfig{}
			}
			existing.Config.Authentication = in.GetRealm().GetConfig().GetAuthentication()
		case "labels":
			existing.Labels = in.GetRealm().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	// kit#586: warnings describe the realm the write leaves, after it
	// succeeds.
	sendWarnings(ctx, f.policyWarningsLocked(in.GetName()))
	return proto.Clone(existing).(*corepb.Realm), nil
}

func (f *fakeIdentityServer) DeleteRealm(ctx context.Context, in *identitypb.DeleteRealmRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.realms[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	delete(f.realms, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Audience and Client (issuer-scoped; the console pair guard needs) ---

// --- Issuer (tenant-scoped; an endpoint's kit_token names one) ---

// admitSelectorLocked refuses a routing selector kit could not honour
// (apis v0.11.0), naming the field the way kit does. It covers the
// structural rules; kit also compiles each condition and refuses a domain
// an earlier condition-less rule already takes, which the fake does not.
// The caller holds f.mu.
func (f *fakeIdentityServer) admitSelectorLocked(tenant string, s *corepb.MultiRealmProviderSelector) error {

	if s == nil {
		return nil
	}

	const prefix = "config.multi_realm_provider_selector"
	invalid := func(field, format string, args ...any) error {
		return status.Errorf(codes.InvalidArgument, "%s%s: %s", prefix, field, fmt.Sprintf(format, args...))
	}

	served := map[string]bool{}
	for i, r := range s.GetRealmNames() {
		if _, ok := f.realms[r]; !ok || !strings.HasPrefix(r, tenant+"/") {
			return invalid(fmt.Sprintf(".realm_names[%d]", i), "%q names no realm in this tenant", r)
		}
		served[r] = true
	}

	target := func(field string, t *corepb.RoutingTarget) error {
		if !served[t.GetRealmName()] {
			return invalid(field+".realm_name", "%q is not one of realm_names", t.GetRealmName())
		}
		if p := t.GetProviderName(); p != "" {
			if _, ok := f.providers[p]; !ok || !strings.HasPrefix(p, t.GetRealmName()+"/providers/") {
				return invalid(field+".provider_name", "%q names no provider in %s", p, t.GetRealmName())
			}
		}
		return nil
	}

	if s.GetDefaultTarget() == nil {
		return invalid(".default_target", "required")
	}
	if err := target(".default_target", s.GetDefaultTarget()); err != nil {
		return err
	}

	username := s.GetIdentifier().GetKind() == corepb.IdentifierField_USERNAME
	names := map[string]bool{}
	for i, r := range s.GetRules() {
		field := fmt.Sprintf(".rules[%d]", i)
		switch {
		case r.GetName() == "":
			return invalid(field+".name", "required")
		case names[r.GetName()]:
			return invalid(field+".name", "%q is already the name of an earlier rule", r.GetName())
		case len(r.GetDomains()) == 0 && r.GetCondition() == "":
			return invalid(field, "a rule needs domains, a condition, or both")
		case username && len(r.GetDomains()) > 0:
			return invalid(field+".domains", "a USERNAME identifier has no domain to match")
		}
		names[r.GetName()] = true
		for j, d := range r.GetDomains() {
			host := strings.TrimPrefix(d, "*.")
			if d != strings.ToLower(d) || host == "" || strings.ContainsAny(host, "*@/ ") || !strings.Contains(host, ".") {
				return invalid(fmt.Sprintf("%s.domains[%d]", field, j), "%q is not a lower-case domain or *.domain", d)
			}
		}
		if r.GetTarget() == nil {
			return invalid(field+".target", "required")
		}
		if err := target(field+".target", r.GetTarget()); err != nil {
			return err
		}
	}

	return nil
}

func (f *fakeIdentityServer) GetIssuer(ctx context.Context, in *identitypb.GetIssuerRequest) (*corepb.Issuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	i, ok := f.issuers[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "issuer %q not found", in.GetName())
	}
	return proto.Clone(i).(*corepb.Issuer), nil
}

func (f *fakeIdentityServer) CreateIssuer(ctx context.Context, in *identitypb.CreateIssuerRequest) (*corepb.Issuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitSelectorLocked(in.GetParent(), in.GetIssuer().GetConfig().GetMultiRealmProviderSelector()); err != nil {
		return nil, err
	}
	i := proto.Clone(in.GetIssuer()).(*corepb.Issuer)
	i.Name = in.GetParent() + "/issuers/" + f.nextID("i")
	f.issuers[i.GetName()] = i
	return proto.Clone(i).(*corepb.Issuer), nil
}

func (f *fakeIdentityServer) PatchIssuer(ctx context.Context, in *identitypb.PatchIssuerRequest) (*corepb.Issuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.issuers[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "issuer %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "labels":
			existing.Labels = in.GetIssuer().GetLabels()
		case "domain_name":
			existing.DomainName = in.GetIssuer().GetDomainName()
		case "path":
			existing.Path = in.GetIssuer().GetPath()
		case "config":
			if err := f.admitSelectorLocked(tenantOf(in.GetName()), in.GetIssuer().GetConfig().GetMultiRealmProviderSelector()); err != nil {
				return nil, err
			}
			existing.Config = in.GetIssuer().GetConfig()
		default:
			if strings.HasPrefix(path, "config.multiRealmProviderSelector.") || strings.HasPrefix(path, "config.multi_realm_provider_selector.") {
				return nil, status.Errorf(codes.InvalidArgument,
					"update_mask path %q is below config.multiRealmProviderSelector, which is written whole", path)
			}
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Issuer), nil
}

func (f *fakeIdentityServer) DeleteIssuer(ctx context.Context, in *identitypb.DeleteIssuerRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.issuers[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "issuer %q not found", in.GetName())
	}
	for name, e := range f.endpoints {
		if e.GetAuth().GetKitToken().GetIssuer() == in.GetName() {
			return nil, status.Errorf(codes.FailedPrecondition,
				"issuer %q is in use by endpoint %s (auth.kit_token.issuer)", in.GetName(), name)
		}
	}
	delete(f.issuers, in.GetName())
	return &emptypb.Empty{}, nil
}

func (f *fakeIdentityServer) GetAudience(ctx context.Context, in *identitypb.GetAudienceRequest) (*corepb.Audience, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	a, ok := f.audiences[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "audience %q not found", in.GetName())
	}
	return proto.Clone(a).(*corepb.Audience), nil
}

func (f *fakeIdentityServer) CreateAudience(ctx context.Context, in *identitypb.CreateAudienceRequest) (*corepb.Audience, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	a := proto.Clone(in.GetAudience()).(*corepb.Audience)
	a.Name = in.GetParent() + "/audiences/" + f.nextID("a")
	f.audiences[a.GetName()] = a
	return proto.Clone(a).(*corepb.Audience), nil
}

func (f *fakeIdentityServer) PatchAudience(ctx context.Context, in *identitypb.PatchAudienceRequest) (*corepb.Audience, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.audiences[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "audience %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetAudience().GetDisplayName()
		case "labels":
			existing.Labels = in.GetAudience().GetLabels()
		case "config":
			existing.Config = in.GetAudience().GetConfig()
		case "appearance_profile_id":
			existing.AppearanceProfileId = in.GetAudience().GetAppearanceProfileId()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Audience), nil
}

func (f *fakeIdentityServer) DeleteAudience(ctx context.Context, in *identitypb.DeleteAudienceRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.audiences[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "audience %q not found", in.GetName())
	}
	delete(f.audiences, in.GetName())
	return &emptypb.Empty{}, nil
}

func (f *fakeIdentityServer) GetClient(ctx context.Context, in *identitypb.GetClientRequest) (*corepb.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c, ok := f.clients[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetName())
	}
	return proto.Clone(c).(*corepb.Client), nil
}

func (f *fakeIdentityServer) CreateClient(ctx context.Context, in *identitypb.CreateClientRequest) (*corepb.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c := proto.Clone(in.GetClient()).(*corepb.Client)
	if err := f.admitAudience(in.GetParent(), c.GetAudienceId()); err != nil {
		return nil, err
	}
	if err := admitClient(c); err != nil {
		return nil, err
	}
	c.Name = in.GetParent() + "/clients/" + f.nextID("c")
	f.clients[c.GetName()] = c
	return proto.Clone(c).(*corepb.Client), nil
}

// admitAudience is kit#620's scope admission for Client.audience_id: an
// audience of the client's own issuer, by bare id. A full name, a missing
// audience and one under another issuer get the same refusal, as in kit.
// Unset passes; the fake does not model kit's required-field check.
func (f *fakeIdentityServer) admitAudience(issuer, id string) error {
	if id == "" {
		return nil
	}
	if _, ok := f.audiences[issuer+"/audiences/"+id]; !ok || strings.Contains(id, "/") {
		return status.Errorf(codes.InvalidArgument, "audience_id: no audience %s in scope", id)
	}
	return nil
}

func (f *fakeIdentityServer) PatchClient(ctx context.Context, in *identitypb.PatchClientRequest) (*corepb.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	stored, ok := f.clients[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetName())
	}
	existing := proto.Clone(stored).(*corepb.Client)
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetClient().GetDisplayName()
		case "audience_id":
			issuer := in.GetName()[:strings.LastIndex(in.GetName(), "/clients/")]
			if err := f.admitAudience(issuer, in.GetClient().GetAudienceId()); err != nil {
				return nil, err
			}
			existing.AudienceId = in.GetClient().GetAudienceId()
		case "grant_types":
			existing.GrantTypes = in.GetClient().GetGrantTypes()
		case "kind":
			existing.Kind = in.GetClient().GetKind()
		case "token_endpoint_auth_method":
			existing.TokenEndpointAuthMethod = in.GetClient().GetTokenEndpointAuthMethod()
		case "status":
			existing.Status = in.GetClient().GetStatus()
		case "expires_at":
			existing.ExpiresAt = in.GetClient().GetExpiresAt()
		case "config":
			existing.Config = in.GetClient().GetConfig()
		case "labels":
			existing.Labels = in.GetClient().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	if err := admitClient(existing); err != nil {
		return nil, err
	}
	f.clients[in.GetName()] = existing
	return proto.Clone(existing).(*corepb.Client), nil
}

func (f *fakeIdentityServer) DeleteClient(ctx context.Context, in *identitypb.DeleteClientRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.clients[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "client %q not found", in.GetName())
	}
	delete(f.clients, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Provider (realm-scoped; carries the SAML SP-role config) ---

func (f *fakeIdentityServer) GetProvider(ctx context.Context, in *identitypb.GetProviderRequest) (*corepb.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	p, ok := f.providers[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "provider %q not found", in.GetName())
	}
	return proto.Clone(p).(*corepb.Provider), nil
}

// oauthReservedParams are the authorization parameters kit sets itself,
// which authorization_params may not override (matched without case).
var oauthReservedParams = map[string]bool{
	"client_id": true, "redirect_uri": true, "response_type": true, "scope": true, "state": true,
	"nonce": true, "code_challenge": true, "code_challenge_method": true, "access_type": true,
}

// notFederated are the provider types with no upstream identity, which kit
// refuses link_by_verified_email on (kit's model.notFederated).
var notFederated = map[string]bool{
	"usernamePassword": true, "magicLink": true, "smsCode": true, "passkey": true, "dummy": true,
}

// admitProvider refuses the configs kit refuses: a config whose type is not
// the provider type's; a magic link outside its bounds (a 6–8 digit code,
// identifiers matched on email only); an OAuth-family config overriding a
// parameter kit sets itself; and an oauth config without a userinfo
// identifier_source or a claim map; and link_by_verified_email (kit#633) or
// trust_upstream_email_verified (kit#663) on a type with no upstream. kit also checks a claim map against the
// type's protocol, which the fake does not; and it requires no client_id or
// secret reference, so neither does the fake.
func admitProvider(p *corepb.Provider) error {

	if p.GetLinkByVerifiedEmail() && notFederated[p.GetProviderType()] {
		return status.Errorf(codes.InvalidArgument,
			"link_by_verified_email is for a federated provider; %s is not one", p.GetProviderType())
	}
	if p.GetTrustUpstreamEmailVerified() && notFederated[p.GetProviderType()] {
		return status.Errorf(codes.InvalidArgument,
			"trust_upstream_email_verified is for a federated provider; %s is not one", p.GetProviderType())
	}

	want := map[string]proto.Message{
		"magicLink": &corepb.ProviderMagicLink{},
		"passkey":   &corepb.ProviderPasskey{},
		"google":    &corepb.ProviderGoogle{},
		"microsoft": &corepb.ProviderMicrosoft{},
		"github":    &corepb.ProviderGitHub{},
		"facebook":  &corepb.ProviderFacebook{},
		"apple":     &corepb.ProviderApple{},
		"oidc":      &corepb.ProviderOidc{},
		"okta":      &corepb.ProviderOkta{},
		"auth0":     &corepb.ProviderAuth0{},
		"linkedin":  &corepb.ProviderLinkedIn{},
		"oauth":     &corepb.ProviderOAuth{},
	}[p.GetProviderType()]
	if want == nil || p.GetConfig() == nil {
		return nil
	}
	if err := p.GetConfig().UnmarshalTo(want); err != nil {
		return status.Errorf(codes.InvalidArgument, "a %s provider takes a %s config: %v",
			p.GetProviderType(), want.ProtoReflect().Descriptor().Name(), err)
	}

	if ap, ok := want.(interface{ GetAuthorizationParams() map[string]string }); ok {
		for k := range ap.GetAuthorizationParams() {
			if oauthReservedParams[strings.ToLower(k)] {
				return status.Errorf(codes.InvalidArgument, "authorization_params may not set %q: kit sets it", k)
			}
		}
	}

	if oa, ok := want.(*corepb.ProviderOAuth); ok {
		if !strings.HasPrefix(oa.GetIdentifierSource(), "userinfo.") || len(oa.GetIdentifierSource()) == len("userinfo.") {
			return status.Errorf(codes.InvalidArgument, "identifier_source must be userinfo.<path>, got %q", oa.GetIdentifierSource())
		}
		if len(oa.GetClaimMap().GetMap()) == 0 && len(oa.GetClaimMap().GetStatic()) == 0 {
			return status.Errorf(codes.InvalidArgument, "claim_map is required for oauth: it has no default")
		}
	}

	if ml, ok := want.(*corepb.ProviderMagicLink); ok {
		if n := ml.GetCodeLength(); n != 0 && (n < 6 || n > 8) {
			return status.Errorf(codes.InvalidArgument, "code_length must be 6 to 8, got %d", n)
		}
		if a := ml.GetIdentifierAttribute(); a != "" && a != "email" {
			return status.Errorf(codes.InvalidArgument, "identifier_attribute must be email, got %q", a)
		}
	}

	return nil
}

func (f *fakeIdentityServer) CreateProvider(ctx context.Context, in *identitypb.CreateProviderRequest) (*corepb.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitLocked(in); err != nil {
		return nil, err
	}
	if err := admitProvider(in.GetProvider()); err != nil {
		return nil, err
	}
	p := proto.Clone(in.GetProvider()).(*corepb.Provider)
	p.Name = in.GetParent() + "/providers/" + f.nextID("p")
	f.providers[p.GetName()] = p
	return proto.Clone(p).(*corepb.Provider), nil
}

func (f *fakeIdentityServer) PatchProvider(ctx context.Context, in *identitypb.PatchProviderRequest) (*corepb.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitLocked(in); err != nil {
		return nil, err
	}
	existing, ok := f.providers[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "provider %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetProvider().GetDisplayName()
		case "provider_type":
			existing.ProviderType = in.GetProvider().GetProviderType()
		case "config":
			existing.Config = in.GetProvider().GetConfig()
		case "labels":
			existing.Labels = in.GetProvider().GetLabels()
		case "link_by_verified_email":
			existing.LinkByVerifiedEmail = in.GetProvider().GetLinkByVerifiedEmail()
		case "trust_upstream_email_verified":
			existing.TrustUpstreamEmailVerified = in.GetProvider().GetTrustUpstreamEmailVerified()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	// kit validates the row the patch produces, not the patch.
	if err := admitProvider(existing); err != nil {
		return nil, err
	}
	return proto.Clone(existing).(*corepb.Provider), nil
}

func (f *fakeIdentityServer) DeleteProvider(ctx context.Context, in *identitypb.DeleteProviderRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.providers[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "provider %q not found", in.GetName())
	}
	delete(f.providers, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Certificate (tenant-scoped; the SAML trust anchors) ---

func (f *fakeIdentityServer) GetCertificate(ctx context.Context, in *identitypb.GetCertificateRequest) (*corepb.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	c, ok := f.certs[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "certificate %q not found", in.GetName())
	}
	return proto.Clone(c).(*corepb.Certificate), nil
}

func (f *fakeIdentityServer) ListCertificates(ctx context.Context, in *identitypb.ListCertificatesRequest) (*identitypb.ListCertificatesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	res := &identitypb.ListCertificatesResponse{}
	for name, c := range f.certs {
		if strings.HasPrefix(name, in.GetParent()+"/") {
			res.Certificates = append(res.Certificates, proto.Clone(c).(*corepb.Certificate))
		}
	}
	return res, nil
}

// CreateCertificate mirrors kit's two modes off one field: a non-empty
// import_certificate_pem imports the partner's certificate, an empty one
// mints a key pair. Either way the input-only fields are consumed and never
// stored, which is what the provider's InputOnly markers are built against.
func (f *fakeIdentityServer) CreateCertificate(ctx context.Context, in *identitypb.CreateCertificateRequest) (*corepb.Certificate, error) {

	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	c := proto.Clone(in.GetCertificate()).(*corepb.Certificate)
	f.lastMint = proto.Clone(c).(*corepb.Certificate)

	c.Name = in.GetParent() + "/certificates/" + f.nextID("cert")
	c.KeyId = f.nextID("key")
	c.Status = corepb.CertificateStatus_CERTIFICATE_STATUS_ACTIVE

	if pem := c.GetImportCertificatePem(); pem != "" {
		c.Origin = corepb.CertificateOrigin_CERTIFICATE_ORIGIN_IMPORTED
		c.HasPrivateKey = false
		c.CertificatePem = pem
		c.Subject = "CN=partner"
	} else {
		c.Origin = corepb.CertificateOrigin_CERTIFICATE_ORIGIN_GENERATED
		c.HasPrivateKey = true
		c.CertificatePem = "-----BEGIN CERTIFICATE-----\nminted\n-----END CERTIFICATE-----\n"
		cn := c.GetSubjectCommonName()
		if cn == "" {
			cn = c.GetDisplayName()
		}
		c.Subject = "CN=" + cn
	}

	days := c.GetValidityDays()
	if days == 0 {
		days = 825
	}
	c.NotBefore = timestamppb.New(certNotBefore)
	c.NotAfter = timestamppb.New(certNotBefore.AddDate(0, 0, int(days)))
	c.FingerprintSha256 = fmt.Sprintf("%064x", f.seq)

	// Input only: kit reads these off the create request and never writes
	// them to the row, so a read can never echo them back.
	c.SubjectCommonName = ""
	c.ValidityDays = 0
	c.KeySize = 0
	c.ImportCertificatePem = ""

	f.certs[c.GetName()] = c
	return proto.Clone(c).(*corepb.Certificate), nil
}

func (f *fakeIdentityServer) PatchCertificate(ctx context.Context, in *identitypb.PatchCertificateRequest) (*corepb.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.certs[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "certificate %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetCertificate().GetDisplayName()
		case "labels":
			existing.Labels = in.GetCertificate().GetLabels()
		case "status":
			existing.Status = in.GetCertificate().GetStatus()
		case "use":
			existing.Use = in.GetCertificate().GetUse()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Certificate), nil
}

func (f *fakeIdentityServer) DeleteCertificate(ctx context.Context, in *identitypb.DeleteCertificateRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.certs[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "certificate %q not found", in.GetName())
	}
	for name, e := range f.endpoints {
		if e.GetTls().GetClientCertificate() == in.GetName() {
			return nil, status.Errorf(codes.FailedPrecondition,
				"certificate %q is in use by endpoint %s (tls.client_certificate)", in.GetName(), name)
		}
	}
	delete(f.certs, in.GetName())
	return &emptypb.Empty{}, nil
}

// harness boots the fake gRPC server plus a fake OAuth token endpoint and
// renders the provider block pointing at them.
type harness struct {
	fake     *fakeIdentityServer
	access   *fakeAccessServer
	grpcAddr string
	// guard is guard-control, on its own listener as in a deployment.
	guard       *fakeGuardServer
	guardAddr   string
	tokenServer *httptest.Server
	tokenCalls  int
}

func newHarness(t *testing.T) *harness {

	t.Helper()

	h := &harness{fake: newFakeIdentityServer(), access: newFakeAccessServer(), guard: newFakeGuardServer()}

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.grpcAddr = lis.Addr().String()

	s := grpc.NewServer()
	identitypb.RegisterAuthwiseIdentityServiceServer(s, h.fake)
	accesspb.RegisterAuthwiseAccessServiceServer(s, h.access)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	guardLis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.guardAddr = guardLis.Addr().String()
	gs := grpc.NewServer()
	guardpb.RegisterGuardControlServiceServer(gs, h.guard)
	guardpb.RegisterGuardTenantAdminServiceServer(gs, h.guard)
	guardpb.RegisterGuardNodeServiceServer(gs, h.guard)
	go func() { _ = gs.Serve(guardLis) }()
	t.Cleanup(gs.Stop)

	h.tokenServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.tokenCalls++
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"access_token":"acc-test-token","expires_in":3600}`)
	}))
	t.Cleanup(h.tokenServer.Close)

	return h
}

// providerConfig renders the provider block for the fake stack. Scope
// defaults live at the provider level; per-resource overrides are part of
// the individual test configs.
func (h *harness) providerConfig() string {
	return fmt.Sprintf(`
provider "authwise" {
  endpoint       = %q
  guard_endpoint = %q
  insecure       = true
  token_url     = %q
  client_id     = "acc-client"
  client_secret = "acc-secret"
  audience      = "https://api.test"

  tenant_id   = "t-1"
  issuer_id   = "i-1"
  audience_id = "a-1"
}
`, h.grpcAddr, h.guardAddr, h.tokenServer.URL+"/oauth/token")
}
