package provider

import (
	"context"
	"slices"
	"strings"
	"sync"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// featureVocabulary is the install feature a kit with kit#968's names lists
// in GetInstallCapabilities. This provider speaks the names before them, so
// such a kit answers every renamed call with Unimplemented.
const featureVocabulary = "identity.vocabulary"

// attrName is the AIP name field.
const attrName = "name"

// getInstallCapabilitiesMethod is the check's own call, which the check
// must not check.
var getInstallCapabilitiesMethod = identitypb.AuthwiseIdentityService_GetInstallCapabilities_FullMethodName

// kitCompat refuses to talk to a kit that has kit#968's names. It asks
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
}

func newKitCompat(providerVersion, tenantID string) *kitCompat {
	return &kitCompat{providerVersion: providerVersion, tenantID: tenantID}
}

// check returns the refusal, if any, for a call carrying req.
func (k *kitCompat) check(ctx context.Context, method string, req any, cc *grpc.ClientConn) error {

	if method == getInstallCapabilitiesMethod {
		return nil
	}

	k.mu.Lock()
	defer k.mu.Unlock()
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
		if slices.Contains(got.GetFeatures(), featureVocabulary) {
			k.err = k.refusal(got.GetKitVersion())
		}
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		// Not an answer: the next call asks again.
		return nil
	default:
		// Unimplemented is a kit older than the question, which this
		// provider serves; any other refusal, such as PermissionDenied, is a
		// check that cannot be made, and does not block.
	}
	k.done = true
	return k.err
}

// upgradeNotes is where 0.35.0's upgrade notes are: its BREAKING CHANGES.
const upgradeNotes = "https://git.authwise.com/authwise/terraform-provider-authwise/-/blob/main/CHANGELOG.md"

func (k *kitCompat) refusal(kitVersion string) error {
	installed := "a kit with kit#968's names"
	if v := strings.TrimPrefix(kitVersion, "v"); v != "" {
		installed = "kit " + v
	}
	return status.Errorf(codes.FailedPrecondition,
		"this provider (%s) is too old for kit 1.87.0 or later, which renamed clients to applications (kit#968); this install runs %s. "+
			"Upgrade to provider 0.35.0 and add the moved blocks: %s (0.35.0, BREAKING CHANGES).",
		k.providerVersion, installed, upgradeNotes)
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
