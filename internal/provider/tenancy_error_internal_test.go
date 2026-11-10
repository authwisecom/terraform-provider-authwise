package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTenancyErrorDetail: a refused credential at the tenancy service says
// that a home service account never reaches it (tf#71, HA-D5); any other
// error is left as kit wrote it.
func TestTenancyErrorDetail(t *testing.T) {

	for _, c := range []codes.Code{codes.Unauthenticated, codes.PermissionDenied} {
		got := tenancyErrorDetail(status.Error(c, "unauthenticated"))
		assert.Contains(t, got, "home service account")
		assert.Contains(t, got, "central service account the tenant owner created")
	}
	for _, err := range []error{status.Error(codes.InvalidArgument, "bad"), errors.New("plain")} {
		assert.Equal(t, err.Error(), tenancyErrorDetail(err))
	}
}
