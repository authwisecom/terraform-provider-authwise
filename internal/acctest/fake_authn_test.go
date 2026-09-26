package acctest_test

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The kit#544 surface: secrets and the references to them, endpoints (whose
// auth names secrets) and realm factors (whose Duo config does).

var secretRefName = (&corepb.SecretRef{}).ProtoReflect().Descriptor().FullName()

// secretRefs returns every SecretRef name reachable from m, unpacking Anys
// on the way — the fake's version of kit's secretstore.FindRefs, which finds
// a reference by its type wherever it sits.
func secretRefs(m proto.Message) []string {

	var out []string

	var walk func(protoreflect.Message)
	walk = func(msg protoreflect.Message) {

		if msg.Descriptor().FullName() == secretRefName {
			if name := msg.Get(msg.Descriptor().Fields().ByName("name")).String(); name != "" {
				out = append(out, name)
			}
			return
		}

		if a, ok := msg.Interface().(*anypb.Any); ok {
			if inner, err := a.UnmarshalNew(); err == nil {
				walk(inner.ProtoReflect())
			}
			return
		}

		msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			switch {
			case fd.IsMap():
				if fd.MapValue().Kind() == protoreflect.MessageKind {
					v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
						walk(mv.Message())
						return true
					})
				}
			case fd.IsList() && fd.Kind() == protoreflect.MessageKind:
				for i := 0; i < v.List().Len(); i++ {
					walk(v.List().Get(i).Message())
				}
			case fd.Kind() == protoreflect.MessageKind:
				walk(v.Message())
			}
			return true
		})
	}

	walk(m.ProtoReflect())

	return out
}

// admitLocked refuses a write whose references name no secret, as kit's
// SecretAdmission does; the caller holds f.mu.
func (f *fakeIdentityServer) admitLocked(m proto.Message) error {
	for _, ref := range secretRefs(m) {
		if _, ok := f.secrets[ref]; !ok {
			return status.Errorf(codes.InvalidArgument, "secret reference %q names no secret in this tenant", ref)
		}
	}
	return nil
}

// referrersLocked returns the names of the objects referencing secret.
func (f *fakeIdentityServer) referrersLocked(secret string) []string {

	var out []string
	add := func(name string, m proto.Message) {
		for _, ref := range secretRefs(m) {
			if ref == secret {
				out = append(out, name)
				return
			}
		}
	}

	for name, p := range f.providers {
		add(name, p)
	}
	for name, e := range f.endpoints {
		add(name, e)
	}
	for name, fa := range f.factors {
		add(name, fa)
	}
	if f.pinned[secret] {
		out = append(out, "out-of-band")
	}

	sort.Strings(out)
	return out
}

// --- Secret ---

func (f *fakeIdentityServer) GetSecret(ctx context.Context, in *identitypb.GetSecretRequest) (*corepb.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	s, ok := f.secrets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "secret %q not found", in.GetName())
	}
	return proto.Clone(s).(*corepb.Secret), nil
}

func (f *fakeIdentityServer) CreateSecret(ctx context.Context, in *identitypb.CreateSecretRequest) (*corepb.Secret, error) {

	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	s := proto.Clone(in.GetSecret()).(*corepb.Secret)
	external := s.GetSource().GetExternal() != nil

	switch {
	case external && in.GetPayload() != nil:
		return nil, status.Error(codes.InvalidArgument, "an external secret takes no payload")
	case !external && in.GetPayload().GetText() == "":
		return nil, status.Error(codes.InvalidArgument, "an inline secret needs a payload")
	}

	s.Name = in.GetParent() + "/secrets/" + f.nextID("s")
	s.Version = 1
	s.UpdatedAt = timestamppb.New(certNotBefore)
	if s.GetSource() == nil {
		s.Source = &corepb.SecretSource{Kind: &corepb.SecretSource_Inline{Inline: &corepb.SecretSourceInline{}}}
	}

	f.secrets[s.GetName()] = s
	if !external {
		f.material[s.GetName()] = []string{in.GetPayload().GetText()}
	}

	return proto.Clone(s).(*corepb.Secret), nil
}

func (f *fakeIdentityServer) PatchSecret(ctx context.Context, in *identitypb.PatchSecretRequest) (*corepb.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.secrets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "secret %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetSecret().GetDisplayName()
		case "description":
			existing.Description = in.GetSecret().GetDescription()
		case "labels":
			existing.Labels = in.GetSecret().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Secret), nil
}

func (f *fakeIdentityServer) AddSecretVersion(ctx context.Context, in *identitypb.AddSecretVersionRequest) (*corepb.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.secrets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "secret %q not found", in.GetName())
	}
	if existing.GetSource().GetExternal() != nil {
		return nil, status.Error(codes.FailedPrecondition, "an external secret has no versions in kit")
	}
	if in.GetPayload().GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "a new version needs a payload")
	}
	existing.Version++
	f.material[in.GetName()] = append(f.material[in.GetName()], in.GetPayload().GetText())
	return proto.Clone(existing).(*corepb.Secret), nil
}

// DeleteSecret refuses while anything references the secret, naming what
// does, with kit's code for it.
func (f *fakeIdentityServer) DeleteSecret(ctx context.Context, in *identitypb.DeleteSecretRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.secrets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "secret %q not found", in.GetName())
	}
	if holders := f.referrersLocked(in.GetName()); len(holders) > 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"secret %s is referenced by %d object(s) (%s); point them at another secret before deleting this one",
			in.GetName(), len(holders), strings.Join(holders, ", "))
	}
	delete(f.secrets, in.GetName())
	delete(f.material, in.GetName())
	return &emptypb.Empty{}, nil
}

// secretMaterial is the test-facing view of what the provider sent.
func (f *fakeIdentityServer) secretMaterial(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.material[name]...)
}

// onlySecretName returns the single secret's name.
func (f *fakeIdentityServer) onlySecretName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name := range f.secrets {
		return name
	}
	return ""
}

// --- Endpoint ---

// admitEndpointLocked refuses what kit's endpoint validation refuses
// (kit#603, apis v0.9.0): an address of the wrong shape for its transport,
// insecure on REST, tls on an insecure row, a timeout outside 100 ms–60 s,
// and references to an issuer or certificate this tenant does not hold. The
// caller holds f.mu. The patch path validates a row it has already
// modified, which kit does inside a transaction the fake does not have.
func (f *fakeIdentityServer) admitEndpointLocked(tenant string, e *corepb.Endpoint) error {

	invalid := func(format string, args ...any) error {
		return status.Errorf(codes.InvalidArgument, format, args...)
	}

	switch e.GetEndpointType() {
	case corepb.ENDPOINT_TYPE_REST:
		u, err := url.Parse(e.GetAddress())
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return invalid("a REST address must be an absolute http(s) URL with no userinfo, got %q", e.GetAddress())
		}
		if e.GetInsecure() {
			return invalid("insecure is refused on a REST endpoint: the URL's scheme says whether it is plaintext")
		}
	case corepb.ENDPOINT_TYPE_GRPC:
		if _, _, err := net.SplitHostPort(strings.TrimPrefix(e.GetAddress(), "dns:///")); err != nil {
			return invalid("a gRPC address must be host:port or dns:///host:port, got %q", e.GetAddress())
		}
	}

	if e.GetInsecure() && e.GetTls() != nil {
		return invalid("tls is refused on an insecure endpoint, which has no TLS")
	}

	if t := e.GetTimeout(); t != nil {
		if d := t.AsDuration(); d < 100*time.Millisecond || d > 60*time.Second {
			return invalid("timeout must be between 100ms and 60s, got %s", d)
		}
	}

	if name := e.GetTls().GetClientCertificate(); name != "" {
		c, ok := f.certs[name]
		if !ok || !strings.HasPrefix(name, tenant+"/") {
			return invalid("tls.client_certificate %q names no certificate in this tenant", name)
		}
		if !c.GetHasPrivateKey() {
			return invalid("tls.client_certificate %q holds no private key to present", name)
		}
	}

	if kt := e.GetAuth().GetKitToken(); kt != nil {
		if _, ok := f.issuers[kt.GetIssuer()]; !ok || !strings.HasPrefix(kt.GetIssuer(), tenant+"/") {
			return invalid("auth.kit_token.issuer %q names no issuer in this tenant", kt.GetIssuer())
		}
		if kt.GetAudience() == "" {
			return invalid("auth.kit_token.audience is required")
		}
	}

	return nil
}

// tenantOf returns the tenants/{t} prefix of a tenant-scoped name.
func tenantOf(name string) string {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 2 {
		return name
	}
	return parts[0] + "/" + parts[1]
}

func (f *fakeIdentityServer) GetEndpoint(ctx context.Context, in *identitypb.GetEndpointRequest) (*corepb.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	e, ok := f.endpoints[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "endpoint %q not found", in.GetName())
	}
	return proto.Clone(e).(*corepb.Endpoint), nil
}

func (f *fakeIdentityServer) CreateEndpoint(ctx context.Context, in *identitypb.CreateEndpointRequest) (*corepb.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitLocked(in); err != nil {
		return nil, err
	}
	if err := f.admitEndpointLocked(in.GetParent(), in.GetEndpoint()); err != nil {
		return nil, err
	}
	e := proto.Clone(in.GetEndpoint()).(*corepb.Endpoint)
	e.Name = in.GetParent() + "/endpoints/" + f.nextID("e")
	f.endpoints[e.GetName()] = e
	return proto.Clone(e).(*corepb.Endpoint), nil
}

func (f *fakeIdentityServer) PatchEndpoint(ctx context.Context, in *identitypb.PatchEndpointRequest) (*corepb.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitLocked(in); err != nil {
		return nil, err
	}
	existing, ok := f.endpoints[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "endpoint %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetEndpoint().GetDisplayName()
		case "labels":
			existing.Labels = in.GetEndpoint().GetLabels()
		case "address":
			existing.Address = in.GetEndpoint().GetAddress()
		case "auth":
			existing.Auth = in.GetEndpoint().GetAuth()
		case "endpoint_type":
			existing.EndpointType = in.GetEndpoint().GetEndpointType()
		case "insecure":
			existing.Insecure = in.GetEndpoint().GetInsecure()
		case "tls":
			existing.Tls = in.GetEndpoint().GetTls()
		case "timeout":
			existing.Timeout = in.GetEndpoint().GetTimeout()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	// kit validates the row the patch produces, not the patch.
	if err := f.admitEndpointLocked(tenantOf(in.GetName()), existing); err != nil {
		return nil, err
	}
	return proto.Clone(existing).(*corepb.Endpoint), nil
}

func (f *fakeIdentityServer) DeleteEndpoint(ctx context.Context, in *identitypb.DeleteEndpointRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.endpoints[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "endpoint %q not found", in.GetName())
	}
	if refs := f.endpointReferrersLocked(in.GetName()); len(refs) > 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "endpoint %q is in use by %s %s (%s)",
			in.GetName(), refs[0].GetReferrerType(), refs[0].GetReferrerName(), refs[0].GetField())
	}
	delete(f.endpoints, in.GetName())
	return &emptypb.Empty{}, nil
}

// endpointReferrersLocked finds every row naming the endpoint in any string
// field, Anys unpacked — kit's referrer index, done by brute force. The
// caller holds f.mu.
func (f *fakeIdentityServer) endpointReferrersLocked(endpoint string) []*identitypb.EndpointReferrer {

	var out []*identitypb.EndpointReferrer
	scan := func(kind string, rows map[string]proto.Message) {
		names := make([]string, 0, len(rows))
		for name := range rows {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, field := range stringFieldsEqual(rows[name], endpoint) {
				out = append(out, &identitypb.EndpointReferrer{ReferrerType: kind, ReferrerName: name, Field: field})
			}
		}
	}

	scan("issuer", asMessages(f.issuers))
	scan("client", asMessages(f.clients))
	scan("audience", asMessages(f.audiences))
	scan("realm", asMessages(f.realms))
	scan("provider", asMessages(f.providers))
	scan("factor", asMessages(f.factors))

	return out
}

func asMessages[M proto.Message](rows map[string]M) map[string]proto.Message {
	out := make(map[string]proto.Message, len(rows))
	for k, v := range rows {
		out[k] = v
	}
	return out
}

// stringFieldsEqual returns the dotted paths of the string fields in m
// whose value is want, looking inside Anys.
func stringFieldsEqual(m proto.Message, want string) []string {

	var out []string

	var walk func(msg protoreflect.Message, prefix string)
	walk = func(msg protoreflect.Message, prefix string) {

		if a, ok := msg.Interface().(*anypb.Any); ok {
			if inner, err := a.UnmarshalNew(); err == nil {
				walk(inner.ProtoReflect(), prefix)
			}
			return
		}

		msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			path := prefix + string(fd.Name())
			switch {
			case fd.IsMap() || fd.IsList():
			case fd.Kind() == protoreflect.StringKind:
				if v.String() == want {
					out = append(out, path)
				}
			case fd.Kind() == protoreflect.MessageKind:
				walk(v.Message(), path+".")
			}
			return true
		})
	}

	walk(m.ProtoReflect(), "")

	return out
}

func (f *fakeIdentityServer) ListEndpointReferrers(ctx context.Context, in *identitypb.ListEndpointReferrersRequest) (*identitypb.ListEndpointReferrersResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.endpoints[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "endpoint %q not found", in.GetName())
	}
	return &identitypb.ListEndpointReferrersResponse{Referrers: f.endpointReferrersLocked(in.GetName())}, nil
}

// CheckEndpoint dials the stored row's address over TCP, which is as far as
// the fake goes: reachable means a connection was made. An endpoint that
// cannot be reached is a report, not an error, exactly as kit answers it.
func (f *fakeIdentityServer) CheckEndpoint(ctx context.Context, in *identitypb.CheckEndpointRequest) (*identitypb.CheckEndpointResponse, error) {

	f.mu.Lock()
	f.recordAuth(ctx)
	stored, ok := f.endpoints[in.GetName()]
	if !ok {
		f.mu.Unlock()
		return nil, status.Errorf(codes.NotFound, "endpoint %q not found", in.GetName())
	}
	e := proto.Clone(stored).(*corepb.Endpoint)
	resp := &identitypb.CheckEndpointResponse{AuthScheme: "none"}
	switch auth := e.GetAuth(); {
	case auth.GetBearer() != nil:
		resp.AuthScheme = "bearer"
		_, resp.CredentialResolved = f.secrets[auth.GetBearer().GetToken().GetName()]
	case auth.GetBasic() != nil:
		resp.AuthScheme = "basic"
		_, resp.CredentialResolved = f.secrets[auth.GetBasic().GetPassword().GetName()]
	case auth.GetHeader() != nil:
		resp.AuthScheme = "header"
		_, resp.CredentialResolved = f.secrets[auth.GetHeader().GetValue().GetName()]
	case auth.GetKitToken() != nil:
		resp.AuthScheme = "kit_token"
		_, resp.CredentialResolved = f.issuers[auth.GetKitToken().GetIssuer()]
	}
	f.mu.Unlock()

	host := strings.TrimPrefix(e.GetAddress(), "dns:///")
	if e.GetEndpointType() == corepb.ENDPOINT_TYPE_REST {
		if u, err := url.Parse(e.GetAddress()); err == nil {
			host = u.Host
			if u.Port() == "" {
				host = net.JoinHostPort(u.Hostname(), map[string]string{"http": "80", "https": "443"}[u.Scheme])
			}
		}
	}

	start := time.Now()
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", host)
	resp.LatencyMs = int32(time.Since(start).Milliseconds())
	if err != nil {
		// Unreachable is the answer, not a failure of the RPC.
		resp.Error = err.Error()
		return resp, nil //nolint:nilerr // kit reports a failed dial in the response
	}
	resp.Reachable = true
	resp.ResolvedAddress = conn.RemoteAddr().String()
	_ = conn.Close()

	return resp, nil
}

// --- Factor (realm-scoped) ---

func (f *fakeIdentityServer) GetFactor(ctx context.Context, in *identitypb.GetFactorRequest) (*corepb.Factor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	fa, ok := f.factors[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "factor %q not found", in.GetName())
	}
	return proto.Clone(fa).(*corepb.Factor), nil
}

// CreateFactor defaults status to active, as kit does for a new row.
func (f *fakeIdentityServer) CreateFactor(ctx context.Context, in *identitypb.CreateFactorRequest) (*corepb.Factor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitLocked(in); err != nil {
		return nil, err
	}
	fa := proto.Clone(in.GetFactor()).(*corepb.Factor)
	fa.Name = in.GetParent() + "/factors/" + f.nextID("f")
	if fa.GetStatus() == "" {
		fa.Status = "active"
	}
	f.factors[fa.GetName()] = fa
	sendWarnings(ctx, f.policyWarningsLocked(in.GetParent()))
	return proto.Clone(fa).(*corepb.Factor), nil
}

func (f *fakeIdentityServer) PatchFactor(ctx context.Context, in *identitypb.PatchFactorRequest) (*corepb.Factor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := f.admitLocked(in); err != nil {
		return nil, err
	}
	existing, ok := f.factors[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "factor %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetFactor().GetDisplayName()
		case "labels":
			existing.Labels = in.GetFactor().GetLabels()
		case "config":
			existing.Config = in.GetFactor().GetConfig()
		case "status":
			existing.Status = in.GetFactor().GetStatus()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	sendWarnings(ctx, f.policyWarningsLocked(realmOf(in.GetName())))
	return proto.Clone(existing).(*corepb.Factor), nil
}

func (f *fakeIdentityServer) DeleteFactor(ctx context.Context, in *identitypb.DeleteFactorRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.factors[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "factor %q not found", in.GetName())
	}
	delete(f.factors, in.GetName())
	return &emptypb.Empty{}, nil
}

// onlyFactor returns the single factor on the server.
func (f *fakeIdentityServer) onlyFactor() *corepb.Factor {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fa := range f.factors {
		return proto.Clone(fa).(*corepb.Factor)
	}
	return nil
}

// --- Warnings and the authentication context schema (kit#586) ---

// fakeFactorTypes is the factor-type catalog the fake's build "runs".
var fakeFactorTypes = []*identitypb.AuthenticationFactorType{
	{FactorType: "totp", Enabled: true, Classes: []string{"possession"}, SupportsEnrollment: true, Amr: []string{"otp"}, ConfigMessage: "authwise.types.core.v1alpha1.FactorTOTP"},
	{FactorType: "webauthn", Enabled: true, Classes: []string{"possession", "inherence"}, PhishingResistant: true, SupportsEnrollment: true, Amr: []string{"hwk", "user"}, ConfigMessage: "authwise.types.core.v1alpha1.FactorWebAuthn"},
	{FactorType: "duo", Enabled: true, Classes: []string{"possession"}, Amr: []string{"mfa"}, ConfigMessage: "authwise.types.core.v1alpha1.FactorDuo"},
}

// policyWarningsLocked judges realm's stored policy against its active
// factors, the one case the fake models: a rule requiring a factor type no
// active factor offers. The wording carries a comma and quotes on purpose,
// which is what makes kit's Display String encoding necessary.
func (f *fakeIdentityServer) policyWarningsLocked(realm string) []string {

	r, ok := f.realms[realm]
	if !ok {
		return nil
	}

	active := map[string]bool{}
	for name, fa := range f.factors {
		if strings.HasPrefix(name, realm+"/") && fa.GetStatus() == "active" {
			active[fa.GetFactorType()] = true
		}
	}

	var out []string
	for _, rule := range r.GetConfig().GetAuthentication().GetRules() {
		for _, t := range rule.GetRequire().GetRequiredFactorTypes() {
			if !active[t] {
				out = append(out, fmt.Sprintf("rule %q, requires %s, which no active factor offers", rule.GetName(), t))
			}
		}
	}

	return out
}

// sendWarnings attaches warnings as kit does: one header value each, as an
// RFC 9651 Display String.
func sendWarnings(ctx context.Context, warnings []string) {
	for _, w := range warnings {
		_ = grpc.SetHeader(ctx, metadata.Pairs("authwise-warning", displayString(w)))
	}
}

func displayString(s string) string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	b.WriteString(`%"`)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' || c == '"' || c < 0x20 || c > 0x7e {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
			continue
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

func (f *fakeIdentityServer) GetRealmAuthenticationContextSchema(ctx context.Context, in *identitypb.GetRealmAuthenticationContextSchemaRequest) (*identitypb.RealmAuthenticationContextSchema, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.realms[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "realm %q not found", in.GetName())
	}
	return &identitypb.RealmAuthenticationContextSchema{
		Variables: []*identitypb.AuthenticationContextVariable{
			{Name: "risk.level", Type: "string", Description: "low, medium or high"},
			{Name: "user.groups", Type: "list(string)", Description: "The person's group names"},
		},
		FactorTypes: fakeFactorTypes,
		Warnings:    f.policyWarningsLocked(in.GetName()),
	}, nil
}

// realmOf returns the realm a realm-child name sits in.
func realmOf(name string) string {
	parts := strings.SplitN(name, "/", 5)
	if len(parts) < 4 {
		return ""
	}
	return strings.Join(parts[:4], "/")
}
