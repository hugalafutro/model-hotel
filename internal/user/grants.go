package user

import (
	"fmt"
	"slices"
)

// Grant is a feature key a non-admin user can be given access to. The catalog
// is the single source of truth: the API validates incoming grants against it
// and the frontend renders its checkboxes from /api/users/grants.
type Grant string

// The v1 grant catalog. Everything not covered here stays admin-only.
const (
	GrantChat        Grant = "chat"         // admin chat endpoints + Chat/Arena UI
	GrantUsage       Grant = "usage"        // stats/usage dashboards (read-only)
	GrantLogs        Grant = "logs"         // request logs (routing metadata only)
	GrantModels      Grant = "models"       // models list (read-only)
	GrantVirtualKeys Grant = "virtual_keys" // virtual keys page (full CRUD)
)

// AllGrants lists every valid grant in display order.
func AllGrants() []Grant {
	return []Grant{GrantChat, GrantUsage, GrantLogs, GrantModels, GrantVirtualKeys}
}

// ValidateGrants rejects unknown or duplicate grant keys.
func ValidateGrants(grants []string) error {
	valid := make(map[string]bool, 8)
	for _, g := range AllGrants() {
		valid[string(g)] = true
	}
	seen := make(map[string]bool, len(grants))
	for _, g := range grants {
		if !valid[g] {
			return fmt.Errorf("unknown grant %q", g)
		}
		if seen[g] {
			return fmt.Errorf("duplicate grant %q", g)
		}
		seen[g] = true
	}
	return nil
}

// HasGrant reports whether the grant list contains g. Role checks live in the
// middleware; admins bypass grants entirely and never reach this.
func HasGrant(grants []string, g Grant) bool {
	return slices.Contains(grants, string(g))
}
