package provider

import (
	"strings"
	"sync"
)

// tenantRowLocks serialises this provider's writes to one tenant's row
// (tf#75). kit writes a tenant by reading the row, merging the request and
// writing the whole row back, with no lock: authwise_tenant_config's
// PatchTenant and authwise_tenant_branding's UpdateTenantBranding write the
// same row, and the two run in parallel whenever one apply changes both, so
// one write was lost (on kit develop c69cf567, every time). Holding a lock
// per tenant while either writes makes them take turns. It cannot order a
// write from the console or awctl; that is kit's to fix.
var tenantRowLocks sync.Map

// lockTenantRow takes the tenant's row lock and returns its release. tenant
// is the tenant's name or id; both lock the same row.
func lockTenantRow(tenant string) func() {
	key := tenantsCollection + "/" + trimTenantName(tenant)
	m, _ := tenantRowLocks.LoadOrStore(key, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// trimTenantName is the tenant's id from its name, its id, or the name of
// something directly under it (tenants/t-…/branding).
func trimTenantName(s string) string {
	s = strings.TrimPrefix(s, tenantsCollection+"/")
	id, _, _ := strings.Cut(s, "/")
	return id
}
