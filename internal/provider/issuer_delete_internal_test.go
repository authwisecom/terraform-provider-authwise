package provider

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIssuerDeleteErrorNamesDomains(t *testing.T) {

	st, err := status.New(codes.FailedPrecondition, "issuer has domains").WithDetails(&errdetails.ErrorInfo{
		Domain: authwiseErrorDomain, Reason: "ISSUER_HAS_DOMAINS", Metadata: map[string]string{"count": "2", "hostname": "login.acme.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := status.Convert(issuerDeleteError(st.Err()))
	if got.Code() != codes.FailedPrecondition || len(got.Details()) != 1 {
		t.Fatalf("code and details must survive: %v", got)
	}
	for _, want := range []string{"ISSUER_HAS_DOMAINS", "2 custom domain(s), such as login.acme.com", "depends_on", "issuer has domains"} {
		if !strings.Contains(got.Message(), want) {
			t.Errorf("message %q lacks %q", got.Message(), want)
		}
	}

	other := status.Error(codes.NotFound, "gone")
	if !errors.Is(issuerDeleteError(other), other) || issuerDeleteError(nil) != nil {
		t.Error("other errors pass through unchanged")
	}
}
