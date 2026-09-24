package provider

import (
	"context"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// warningHeader is the response header kit attaches an accepted write's
// warnings to (kit#586): accepted, but probably not what the author meant —
// a rule that can never fire, a requirement no enabled factor satisfies.
// Without this the warning only reaches kit's log, where the person applying
// the configuration never looks.
const warningHeader = "authwise-warning"

type warningsKey struct{}

// warnings collects the kit warnings of the calls made under one context.
type warnings struct {
	mu   sync.Mutex
	msgs []string
}

// withWarnings returns a context whose gRPC calls record their warnings in
// the returned collector.
func withWarnings(ctx context.Context) (context.Context, *warnings) {
	w := &warnings{}
	return context.WithValue(ctx, warningsKey{}, w), w
}

// report appends each collected warning to diags, once each.
func (w *warnings) report(diags *diag.Diagnostics) {

	w.mu.Lock()
	defer w.mu.Unlock()

	seen := map[string]bool{}
	for _, m := range w.msgs {
		if seen[m] {
			continue
		}
		seen[m] = true
		diags.AddWarning("kit accepted the write with a warning", m)
	}
}

// warningInterceptor reads the warning header off every unary call made
// under a withWarnings context; other calls pass through untouched.
func warningInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

	w, ok := ctx.Value(warningsKey{}).(*warnings)
	if !ok {
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	var header metadata.MD
	err := invoker(ctx, method, req, reply, cc, append(opts, grpc.Header(&header))...)

	values := header.Get(warningHeader)
	msgs := make([]string, 0, len(values))
	for _, v := range values {
		msgs = append(msgs, parseDisplayStrings(v)...)
	}

	w.mu.Lock()
	w.msgs = append(w.msgs, msgs...)
	w.mu.Unlock()

	return err
}

// parseDisplayStrings decodes a header value holding one or more RFC 9651
// Display Strings (`%"…"`, comma-separated when a proxy combined repeated
// values): split on the commas outside quotes, strip the quoting,
// percent-decode. A value that is not a Display String is kept verbatim
// rather than dropped.
func parseDisplayStrings(v string) []string {

	items := splitOutsideQuotes(v)
	out := make([]string, 0, len(items))

	for _, item := range items {

		item = strings.TrimSpace(item)
		if !strings.HasPrefix(item, `%"`) || !strings.HasSuffix(item, `"`) || len(item) < 3 {
			if item != "" {
				out = append(out, item)
			}
			continue
		}

		out = append(out, percentDecode(item[2:len(item)-1]))
	}

	return out
}

func splitOutsideQuotes(v string) []string {

	var out []string
	quoted, start := false, 0

	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, v[start:i])
				start = i + 1
			}
		}
	}

	return append(out, v[start:])
}

func percentDecode(s string) string {

	var b strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok := unhex(s[i+1]); ok {
				if lo, ok := unhex(s[i+2]); ok {
					b.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}

	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

var (
	_ resource.Resource                = &warningResource{}
	_ resource.ResourceWithConfigure   = &warningResource{}
	_ resource.ResourceWithImportState = &warningResource{}
)

// warningResource wraps a generated resource whose writes kit may answer
// with warnings, and reports them. authwise_factor is one: kit judges the
// realm's policy against the factor set a create or update leaves, so
// disabling a factor can warn that a rule is now unsatisfiable.
type warningResource struct {
	inner resource.Resource
}

func (r *warningResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *warningResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
}

func (r *warningResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if inner, ok := r.inner.(resource.ResourceWithConfigure); ok {
		inner.Configure(ctx, req, resp)
	}
}

func (r *warningResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx, w := withWarnings(ctx)
	r.inner.Create(ctx, req, resp)
	w.report(&resp.Diagnostics)
}

func (r *warningResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.inner.Read(ctx, req, resp)
}

func (r *warningResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx, w := withWarnings(ctx)
	r.inner.Update(ctx, req, resp)
	w.report(&resp.Diagnostics)
}

func (r *warningResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.inner.Delete(ctx, req, resp)
}

func (r *warningResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if inner, ok := r.inner.(resource.ResourceWithImportState); ok {
		inner.ImportState(ctx, req, resp)
	}
}
