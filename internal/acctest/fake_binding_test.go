package acctest_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// IssuerDomainBinding (kit#911, TENANT_DOMAINS.md): where an issuer
// answers. (domain, path) is unique in the install. The path is "" or
// canonical segments, and its first segment must not be one kit serves on
// every host (kit#876, model.ValidateIssuerPath). An issuer's
// domain_id, path and domain_name are its first binding's, output only
// after create; preferred_binding_id names one of its bindings.

// refusal is a kit refusal with its ErrorInfo, which the provider reads.
func refusal(code codes.Code, reason string, metadata map[string]string, format string, args ...any) error {
	st, err := status.New(code, fmt.Sprintf(format, args...)).WithDetails(&errdetails.ErrorInfo{Domain: "authwise.io", Reason: reason, Metadata: metadata})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func lastSegment(name string) string { return name[strings.LastIndex(name, "/")+1:] }

// issuerPathForm is kit's canonical non-empty issuer path.
var issuerPathForm = regexp.MustCompile(`^(/[^/?#\s]+)+$`)

// reservedIssuerPaths are the first segments kit serves on every host
// (kit d9a781b1, pkg/lib/model/issuer_path.go).
var reservedIssuerPaths = []string{
	"/admin", "/.well-known", "/authorize", "/oauth", "/userinfo", "/api", "/_ui",
	"/assets", "/account", "/login", "/logout", "/recover-password", "/health",
	"/saml", "/recovery", "/magic-link", "/forgot-password", "/uiassets",
}

// admitBindingLocked is kit's bind check for issuer (a full name) on
// domainID at path.
func (f *fakeIdentityServer) admitBindingLocked(issuer, domainID, path string) error {
	if path != "" && !issuerPathForm.MatchString(path) {
		return status.Errorf(codes.InvalidArgument, "path: not a canonical issuer path %q", path)
	}
	for _, r := range reservedIssuerPaths {
		if path == r || strings.HasPrefix(path, r+"/") {
			return status.Errorf(codes.InvalidArgument, "path: issuer path reserved %q: its first segment is served on every host", path)
		}
	}
	d, ok := f.domains[tenantOf(issuer)+"/domains/"+domainID]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "domain_id: no domain %q in %s", domainID, tenantOf(issuer))
	}
	switch d.GetPurpose() {
	case "guard":
		return refusal(codes.FailedPrecondition, "DOMAIN_NOT_BINDABLE", nil, "domain %q is a Guard address", domainID)
	case "console":
		return refusal(codes.FailedPrecondition, "CONSOLE_DOMAIN_NOT_BINDABLE", nil, "domain %q is a console domain", domainID)
	}
	for _, name := range f.bindingOrder {
		b := f.bindings[name]
		if tenantOf(name) == tenantOf(issuer) && b.GetDomainId() == domainID && b.GetPath() == path {
			return refusal(codes.AlreadyExists, "BINDING_TAKEN", map[string]string{"hostname": d.GetHostname(), "path": path, "issuer": b.GetIssuerId()},
				"%s%s already answers for issuer %s", d.GetHostname(), path, b.GetIssuerId())
		}
	}
	return nil
}

// bindLocked writes an admitted binding and returns it.
func (f *fakeIdentityServer) bindLocked(issuer, domainID, path string) *corepb.IssuerDomainBinding {
	d := f.domains[tenantOf(issuer)+"/domains/"+domainID]
	b := &corepb.IssuerDomainBinding{
		Name:      issuer + "/issuer-domain-bindings/" + f.nextID("idb"),
		DomainId:  domainID,
		Path:      path,
		Hostname:  d.GetHostname(),
		Url:       "https://" + d.GetHostname() + path,
		Routable:  !d.GetCustom(),
		IssuerId:  lastSegment(issuer),
		CreatedAt: timestamppb.Now(),
	}
	f.bindings[b.GetName()] = b
	f.bindingOrder = append(f.bindingOrder, b.GetName())
	return b
}

func (f *fakeIdentityServer) unbindLocked(name string) {
	delete(f.bindings, name)
	for i, n := range f.bindingOrder {
		if n == name {
			f.bindingOrder = append(f.bindingOrder[:i], f.bindingOrder[i+1:]...)
			break
		}
	}
	// A preferred binding deleted falls back to the first.
	if i, ok := f.issuers[strings.TrimSuffix(name, "/issuer-domain-bindings/"+lastSegment(name))]; ok && i.GetPreferredBindingId() == lastSegment(name) {
		i.PreferredBindingId = ""
	}
}

func (f *fakeIdentityServer) bindingsOfLocked(issuer string) []*corepb.IssuerDomainBinding {
	var out []*corepb.IssuerDomainBinding
	for _, name := range f.bindingOrder {
		if strings.HasPrefix(name, issuer+"/issuer-domain-bindings/") {
			out = append(out, f.bindings[name])
		}
	}
	return out
}

func (f *fakeIdentityServer) bindingOfLocked(issuer, id string) *corepb.IssuerDomainBinding {
	return f.bindings[issuer+"/issuer-domain-bindings/"+id]
}

// bindingOnDomainLocked resolves a deprecated preferred_domain_id to the
// issuer's binding on that domain.
func (f *fakeIdentityServer) bindingOnDomainLocked(i *corepb.Issuer, domainID string) (string, error) {
	if domainID == "" {
		return "", nil
	}
	for _, b := range f.bindingsOfLocked(i.GetName()) {
		if b.GetDomainId() == domainID {
			return lastSegment(b.GetName()), nil
		}
	}
	return "", status.Errorf(codes.InvalidArgument, "preferred_domain_id: %s is not one of this issuer's domains", domainID)
}

// renderIssuerLocked is the issuer as kit reads it: its first binding as
// its home, its bindings, and the preferred binding's domain.
func (f *fakeIdentityServer) renderIssuerLocked(i *corepb.Issuer) *corepb.Issuer {
	out := proto.Clone(i).(*corepb.Issuer)
	out.DomainId, out.Path, out.DomainName, out.PreferredDomainId, out.Bindings = "", "", "", "", nil
	for idx, b := range f.bindingsOfLocked(i.GetName()) {
		if idx == 0 {
			out.DomainId, out.Path, out.DomainName = b.GetDomainId(), b.GetPath(), b.GetHostname()
		}
		if lastSegment(b.GetName()) == i.GetPreferredBindingId() {
			out.PreferredDomainId = b.GetDomainId()
		}
		out.Bindings = append(out.Bindings, proto.Clone(b).(*corepb.IssuerDomainBinding))
	}
	return out
}

func (f *fakeIdentityServer) CreateIssuerDomainBinding(ctx context.Context, in *identitypb.CreateIssuerDomainBindingRequest) (*corepb.IssuerDomainBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("issuer domain binding", issuerParent, in.GetParent()); err != nil {
		return nil, err
	}
	if _, ok := f.issuers[in.GetParent()]; !ok {
		return nil, status.Errorf(codes.NotFound, "issuer %q not found", in.GetParent())
	}
	if err := f.admitBindingLocked(in.GetParent(), in.GetIssuerDomainBinding().GetDomainId(), in.GetIssuerDomainBinding().GetPath()); err != nil {
		return nil, err
	}
	b := f.bindLocked(in.GetParent(), in.GetIssuerDomainBinding().GetDomainId(), in.GetIssuerDomainBinding().GetPath())
	return proto.Clone(b).(*corepb.IssuerDomainBinding), nil
}

func (f *fakeIdentityServer) GetIssuerDomainBinding(ctx context.Context, in *identitypb.GetIssuerDomainBindingRequest) (*corepb.IssuerDomainBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	b, ok := f.bindings[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "issuer domain binding %q not found", in.GetName())
	}
	return proto.Clone(b).(*corepb.IssuerDomainBinding), nil
}

func (f *fakeIdentityServer) ListIssuerDomainBindings(ctx context.Context, in *identitypb.ListIssuerDomainBindingsRequest) (*identitypb.ListIssuerDomainBindingsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	out := &identitypb.ListIssuerDomainBindingsResponse{}
	for _, b := range f.bindingsOfLocked(in.GetParent()) {
		out.IssuerDomainBindings = append(out.IssuerDomainBindings, proto.Clone(b).(*corepb.IssuerDomainBinding))
	}
	return out, nil
}

func (f *fakeIdentityServer) DeleteIssuerDomainBinding(ctx context.Context, in *identitypb.DeleteIssuerDomainBindingRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.bindings[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "issuer domain binding %q not found", in.GetName())
	}
	f.unbindLocked(in.GetName())
	return &emptypb.Empty{}, nil
}
