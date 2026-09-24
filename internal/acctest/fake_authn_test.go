package acctest_test

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
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
	delete(f.endpoints, in.GetName())
	return &emptypb.Empty{}, nil
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
