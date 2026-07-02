package api

import (
	"context"
	"net/http"
	"time"
)

// This file enforces, on the server, the same read-only contract the dashboard
// already shows for a managed fleet member. When this instance is an actively
// managed member (Front Desk in contact, non-primary, fresh heartbeat), its
// synced entities (providers, virtual keys, custom failover groups, syncable
// settings, user accounts) are declaratively replaced on the next config sync. The UI hides the
// create/edit/delete affordances for those (see web/src/hooks/useManaged.ts), but
// a direct API call, a second tab, or a page loaded before enrollment can still
// reach the write handlers. Such a write would "succeed" and then be silently
// undone on the next sync, which looks like data loss. We refuse it instead.
//
// The guard mirrors readOnlyGuard (readonly.go) but differs in two ways: it is
// dynamic (the managed state is computed per request, not a startup flag), and it
// is mounted only on the synced-entity write routes rather than the whole admin
// surface, so models/discovery, failover sync, config import, fleet announce,
// backups, and auth stay usable on a managed member.

// managedWriteMsg is the 403 body returned when a synced-entity write is refused
// because this instance is a managed fleet member.
const managedWriteMsg = "this instance is managed by the fleet primary; " +
	"providers, virtual keys, failover groups, synced settings, and user " +
	"accounts are replaced on the next sync and cannot be edited here. " +
	"Change them on the primary."

// isManagedMember reports whether this instance is an actively managed fleet
// member: Front Desk is in contact AND this node is a non-primary member with a
// fresh heartbeat (fleet state "member"). It reads the cached _fleet_* settings
// via computeFleetStatus, so it is cheap enough to call on a write request.
// "primary", "warning" (stale heartbeat), and standalone (nil) all return false,
// matching useManaged: an operator is never locked out when Front Desk is away.
//
// The guard is not instantaneous: the _fleet_* reads go through the settings
// cache (~30s TTL), so a node that has just become a member can briefly still
// accept a write. That fail-open-on-stale window is the correct posture here, not
// a bug. The enforcement is defense-in-depth for the read-only UI, not a
// consistency boundary, and any write that slips through is reconciled by the
// next config sync (the primary remains the source of truth).
func isManagedMember(ctx context.Context, fs fleetSettings) bool {
	st := computeFleetStatus(ctx, fs, time.Now())
	return st != nil && st.State == "member"
}

// managedBlocksSyncableSettings reports whether a settings write touching the
// given keys must be refused because this instance is a managed member. A write
// is refused only when at least one key is syncable (config-sync replicates it);
// instance-local keys (Apprise routing, Observability) are always allowed, which
// is why settings cannot use the route-level managedWriteGuard: one PUT carries a
// mix of synced and instance-local keys.
func managedBlocksSyncableSettings(ctx context.Context, fs fleetSettings, keys []string) bool {
	if !isManagedMember(ctx, fs) {
		return false
	}
	for _, k := range keys {
		if isSyncableSetting(k) {
			return true
		}
	}
	return false
}

// managedWriteGuard returns middleware that refuses a request with 403 when this
// instance is a managed fleet member. Mount it ONLY on synced-entity write routes
// (providers, virtual keys, custom failover group CRUD, user accounts): every
// request that reaches it is treated as such a write, so it does no method or
// path inspection.
func managedWriteGuard(fs fleetSettings) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isManagedMember(r.Context(), fs) {
				// nil err + a 4xx code: respondError does not log this (it is a
				// client-facing policy rejection, not a server fault).
				respondError(w, managedWriteMsg, nil, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
