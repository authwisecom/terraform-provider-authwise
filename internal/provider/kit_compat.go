package provider

import (
	"context"
	"slices"
	"strings"
	"sync"

	guardpb "git.authwise.com/authwise/apis/authwise/guardcontrol/v1alpha1"
	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// featureVocabulary is the install feature a kit with kit#968's names lists
// in GetInstallCapabilities. This provider speaks only those names, so a kit
// without it answers every renamed call with Unimplemented.
const featureVocabulary = "identity.vocabulary"

// gatedFeatures are the install features a call needs beyond the
// vocabulary, by the call's full method (train 2026.11.5, plan's ruling):
// the resources that make it refuse on a kit without the feature, and
// everything else in the provider keeps working against that kit. Never
// the kit version: the contract says to read features.
var gatedFeatures = func() map[string]gatedFeature {
	scim := gatedFeature{feature: "identity.scim", what: "SCIM (authwise_scim_connection, authwise_scim_token)"}
	issuers := gatedFeature{feature: "guard.trusted-issuers", what: "trusted issuers (authwise_guard_trusted_issuer, authwise_guard_trusted_issuer_rule)"}
	m := map[string]gatedFeature{}
	for _, method := range []string{
		identitypb.AuthwiseIdentityService_GetScimConnection_FullMethodName,
		identitypb.AuthwiseIdentityService_ListScimConnections_FullMethodName,
		identitypb.AuthwiseIdentityService_CreateScimConnection_FullMethodName,
		identitypb.AuthwiseIdentityService_UpdateScimConnection_FullMethodName,
		identitypb.AuthwiseIdentityService_PatchScimConnection_FullMethodName,
		identitypb.AuthwiseIdentityService_DeleteScimConnection_FullMethodName,
		identitypb.AuthwiseIdentityService_GetScimToken_FullMethodName,
		identitypb.AuthwiseIdentityService_ListScimTokens_FullMethodName,
		identitypb.AuthwiseIdentityService_CreateScimToken_FullMethodName,
		identitypb.AuthwiseIdentityService_DeleteScimToken_FullMethodName,
	} {
		m[method] = scim
	}
	for _, md := range guardpb.GuardTrustedIssuerService_ServiceDesc.Methods {
		m["/"+guardpb.GuardTrustedIssuerService_ServiceDesc.ServiceName+"/"+md.MethodName] = issuers
	}
	return m
}()

// gatedFeature is one install feature a group of calls needs, and how the
// refusal names them.
type gatedFeature struct {
	feature string
	what    string
}

// getInstallCapabilitiesMethod is the check's own call, which the check
// must not check.
var getInstallCapabilitiesMethod = identitypb.AuthwiseIdentityService_GetInstallCapabilities_FullMethodName

// kitCompat refuses to talk to a kit that lacks kit#968's names. It asks
// once per provider run, before the first call to kit, and only asks: the
// answer's features decide, and kit_version appears only in the message
// (the contract says never to compare it). A check that cannot be made (no
// tenant to ask under, PermissionDenied, any other refusal) lets the call
// through, so a narrow credential is never locked out by the check itself.
type kitCompat struct {
	// providerVersion names this provider in the refusal.
	providerVersion string
	// tenantID is the provider's default tenant; without one, the tenant
	// of the first request that names one.
	tenantID string

	mu   sync.Mutex
	done bool
	err  error
	// answered is whether kit said which features it lists; features and
	// kitVersion are its answer. A check that could not be made leaves it
	// false, and no gated call is refused for want of an answer.
	answered   bool
	features   []string
	kitVersion string
}

func newKitCompat(providerVersion, tenantID string) *kitCompat {
	return &kitCompat{providerVersion: providerVersion, tenantID: tenantID}
}

// check returns the refusal, if any, for a call carrying req. cc is kit's
// connection, which the question goes to.
func (k *kitCompat) check(ctx context.Context, method string, req any, cc grpc.ClientConnInterface) error {

	if method == getInstallCapabilitiesMethod {
		return nil
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.askLocked(ctx, req, cc); err != nil {
		return err
	}
	if g, ok := gatedFeatures[method]; ok && k.answered && !slices.Contains(k.features, g.feature) {
		return k.gatedRefusal(g)
	}
	return nil
}

// askLocked asks kit once per run and returns the vocabulary refusal, if
// any.
func (k *kitCompat) askLocked(ctx context.Context, req any, cc grpc.ClientConnInterface) error {

	if k.done {
		return k.err
	}

	tenant := k.tenantID
	if tenant == "" {
		tenant = tenantOf(req)
	}
	if tenant == "" {
		// Nothing to ask under yet; a later call may name a tenant.
		return nil
	}

	got, err := identitypb.NewAuthwiseIdentityServiceClient(cc).GetInstallCapabilities(ctx,
		&identitypb.GetInstallCapabilitiesRequest{Parent: "tenants/" + tenant})
	switch status.Code(err) {
	case codes.OK:
		k.answered, k.features, k.kitVersion = true, got.GetFeatures(), got.GetKitVersion()
		if !slices.Contains(got.GetFeatures(), featureVocabulary) {
			k.err = k.refusal(got.GetKitVersion())
		}
	case codes.Unimplemented:
		// Older than the question itself.
		k.err = k.refusal("")
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		// Not an answer: the next call asks again.
		return nil
	default:
		// A refusal, such as PermissionDenied: the check cannot be made, and
		// does not block.
	}
	k.done = true
	return k.err
}

// gatedRefusal says which feature the install lacks and what needs it.
func (k *kitCompat) gatedRefusal(g gatedFeature) error {
	installed := "this kit"
	if v := strings.TrimPrefix(k.kitVersion, "v"); v != "" {
		installed = "kit " + v
	}
	return status.Errorf(codes.FailedPrecondition,
		"%s needs a kit that lists the %s install feature; this install runs %s, which does not. "+
			"Upgrade kit to use them; the rest of this provider works with it as it is.",
		g.what, g.feature, installed)
}

// refusal is awctl's sentence (custom/vocabulary_gate.go), so both tools
// refuse an old kit in the same words, then what to do about it here.
func (k *kitCompat) refusal(kitVersion string) error {
	installed := "an earlier kit"
	if v := strings.TrimPrefix(kitVersion, "v"); v != "" {
		installed = "kit " + v
	}
	return status.Errorf(codes.FailedPrecondition,
		"this provider (%s) needs kit 1.87.0 or later, which renamed clients to applications; this install runs %s. "+
			"Upgrade kit, or stay on provider 0.34.x until it is upgraded.",
		k.providerVersion, installed)
}

// gatedUnary is for another service's connection (guard-control): only the
// calls gatedFeatures names are checked, and the question goes to kit on
// kitConn.
func (k *kitCompat) gatedUnary(kitConn grpc.ClientConnInterface) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := gatedFeatures[method]; ok {
			if err := k.check(ctx, method, req, kitConn); err != nil {
				return err
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func (k *kitCompat) unary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if err := k.check(ctx, method, req, cc); err != nil {
			return err
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// stream checks before a stream opens. A stream's request is not known
// then, so it asks only with the provider's default tenant or one an
// earlier call named.
func (k *kitCompat) stream() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if err := k.check(ctx, method, nil, cc); err != nil {
			return nil, err
		}
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// tenantOf is the tenant a request names: the leading tenants/{t} of its
// `name` or `parent`, the AIP fields every kit request carries one in.
func tenantOf(req any) string {
	m, ok := req.(proto.Message)
	if !ok || m == nil {
		return ""
	}
	r := m.ProtoReflect()
	for _, field := range []protoreflect.Name{attrName, "parent"} {
		fd := r.Descriptor().Fields().ByName(field)
		if fd == nil || fd.Kind() != protoreflect.StringKind || fd.IsList() {
			continue
		}
		if t := tenantPrefix(r.Get(fd).String()); t != "" {
			return t
		}
	}
	return ""
}

func tenantPrefix(name string) string {
	rest, ok := strings.CutPrefix(name, "tenants/")
	if !ok {
		return ""
	}
	t, _, _ := strings.Cut(rest, "/")
	return t
}
