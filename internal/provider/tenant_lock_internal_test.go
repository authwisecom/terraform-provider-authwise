package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTrimTenantName(t *testing.T) {
	for in, want := range map[string]string{
		"t-03":                  "t-03",
		"tenants/t-03":          "t-03",
		"tenants/t-03/branding": "t-03",
	} {
		assert.Equal(t, want, trimTenantName(in), in)
	}
}

// TestLockTenantRow: the tenant's name, id and branding name take one lock,
// so tenant_config and tenant_branding take turns; another tenant's does
// not wait.
func TestLockTenantRow(t *testing.T) {

	release := lockTenantRow("tenants/t-lock/branding")

	other := make(chan struct{})
	go func() { lockTenantRow("t-other")(); close(other) }()
	select {
	case <-other:
	case <-time.After(time.Second):
		t.Fatal("another tenant's write waited")
	}

	same := make(chan struct{})
	go func() { lockTenantRow("tenants/t-lock")(); close(same) }()
	select {
	case <-same:
		t.Fatal("the tenant's config write did not wait for its branding write")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-same:
	case <-time.After(time.Second):
		t.Fatal("the config write never ran")
	}
}
