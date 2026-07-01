package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// maxAnnounceBody bounds the announce request body. The payload is a handful of
// short fields, so a kilobyte is generous and keeps a malformed caller from
// streaming an unbounded body into memory.
const maxAnnounceBody = 1 << 10

// This file implements the member side of HA Phase 6: surfacing fleet
// membership back onto a member instance's own dashboard. Front Desk announces
// itself to each member on its poll (POST /fleet/announce); the member persists
// the contact as instance-local, non-syncable settings and exposes a computed
// fleet state on its system-status payload (see system.go). A standalone
// instance that Front Desk never contacts stores nothing and shows nothing.
//
// The _fleet_* keys are deliberately NOT in settings.AllowedSettings, so
// config-sync's declarative replace (internal/api/configsync.go) never writes
// or deletes them: they survive a sync the same way apprise/observability keys
// do. Because the allowlist guards SetTx/DeleteKeysTx (but not Set), every
// write of these keys must go through Repository.Set, never SetTx.

const (
	// keyFleetManagedSeenAt is the RFC3339 timestamp of the last Front Desk
	// contact. Its presence is what distinguishes a managed member from a
	// standalone instance; its age drives the liveness window.
	keyFleetManagedSeenAt = "_fleet_managed_seen_at"
	// keyFleetIsPrimary is "true" when this instance is the fleet's config
	// source (the primary the last sync ran from), "false" otherwise.
	keyFleetIsPrimary = "_fleet_is_primary"
	// keyFleetPrimaryName is the display name of the fleet primary, for the
	// dashboard tooltip ("synced from <name>"). Optional.
	keyFleetPrimaryName = "_fleet_primary_name"
	// keyFleetFrontdeskID is an opaque Front Desk identity, display-only.
	// Optional and unused in PR 1 (Front Desk has no self-identity yet).
	keyFleetFrontdeskID = "_fleet_frontdesk_id"
	// keyFleetConfigSyncedAt is the RFC3339 timestamp of the last successful
	// config-sync apply, written post-commit by ConfigSyncHandler.apply.
	keyFleetConfigSyncedAt = "_fleet_config_synced_at"
	// keyFleetLastSourceGen is the highest Front Desk source generation
	// (auto_sync_gen) this member has applied. It is the member-side commit
	// fence: an import carrying an older generation is refused so a stale,
	// out-of-order push (a primary repoint while an import was in flight) can
	// never overwrite a newer config that already landed. Stored as a decimal
	// int64; absent on a member that has never taken a fenced import.
	keyFleetLastSourceGen = "_fleet_last_source_gen"
)

const (
	// fleetManagedTTL bounds "fresh": a member is considered actively managed
	// only while the last announce is younger than this. It is sized at roughly
	// 3x a nominal Front Desk poll (HealthPollSecs defaults to 5s) so a couple
	// of missed announces do not flap the badge, while a stopped Front Desk
	// degrades the line to "warning" within a poll interval or two.
	fleetManagedTTL = 90 * time.Second
	// fleetForgetTTL is the much longer window after which a member that has not
	// heard from Front Desk at all is treated as standalone again (the line
	// disappears rather than sitting on a permanent amber "warning"). This is
	// what makes a member pulled from the fleet self-clear.
	fleetForgetTTL = 24 * time.Hour
)

// fleetSettings is the minimal settings surface this file and system.go need:
// reads for status, a single allowlist-bypassing write for the heartbeat.
// Both are satisfied by *settings.Repository (via the SettingsStore interface).
type fleetSettings interface {
	GetWithDefault(ctx context.Context, key, defaultValue string) string
	Set(ctx context.Context, key, value string) error
	SetMany(ctx context.Context, kvs [][2]string) error
}

// FleetStatus is the member's own view of its fleet membership, surfaced on the
// system-status payload. It is nil (omitted) for a standalone instance.
type FleetStatus struct {
	// State is one of "primary", "member", "warning", or "member_sync_blocked".
	// "member_sync_blocked" is reserved for a later change (when Front Desk
	// forwards a blocked-sync signal) and is not emitted yet.
	State          string `json:"state"`
	IsPrimary      bool   `json:"is_primary"`
	PrimaryName    string `json:"primary_name,omitempty"`
	FrontdeskID    string `json:"frontdesk_id,omitempty"`
	ManagedSeenAt  string `json:"managed_seen_at,omitempty"`
	ConfigSyncedAt string `json:"config_synced_at,omitempty"`
}

// computeFleetStatus derives the member's fleet state from the persisted
// _fleet_* settings and the liveness windows. It returns nil for a standalone
// instance (no recorded contact, or contact older than fleetForgetTTL) so the
// dashboard renders nothing. now is injectable for tests.
func computeFleetStatus(ctx context.Context, s fleetSettings, now time.Time) *FleetStatus {
	seen := s.GetWithDefault(ctx, keyFleetManagedSeenAt, "")
	if seen == "" {
		return nil // never contacted: standalone
	}
	seenAt, err := time.Parse(time.RFC3339, seen)
	if err != nil {
		debuglog.Warn("fleet: unparseable managed-seen-at; treating as standalone", "value", seen)
		return nil
	}
	age := now.Sub(seenAt)
	if age >= fleetForgetTTL {
		return nil // long gone: forget the fleet, behave as standalone again
	}

	fs := &FleetStatus{
		IsPrimary:      s.GetWithDefault(ctx, keyFleetIsPrimary, "false") == "true",
		PrimaryName:    s.GetWithDefault(ctx, keyFleetPrimaryName, ""),
		FrontdeskID:    s.GetWithDefault(ctx, keyFleetFrontdeskID, ""),
		ManagedSeenAt:  seen,
		ConfigSyncedAt: s.GetWithDefault(ctx, keyFleetConfigSyncedAt, ""),
	}
	switch {
	case age >= fleetManagedTTL:
		// Heartbeat stale but not yet forgotten: Front Desk may be down or this
		// member may have been pulled. Either way it is not actively managed.
		fs.State = "warning"
	case fs.IsPrimary:
		fs.State = "primary"
	default:
		fs.State = "member"
	}
	return fs
}

// FleetHandler serves POST /fleet/announce, the admin-authenticated heartbeat
// Front Desk pings on its poll. It is mounted inside the admin-authenticated
// /api group (see Handler.Register), so an unauthenticated caller cannot forge
// a member's fleet state.
type FleetHandler struct {
	settings fleetSettings
}

// NewFleetHandler builds the member-side fleet handler.
func NewFleetHandler(settings fleetSettings) *FleetHandler {
	return &FleetHandler{settings: settings}
}

// Register mounts POST /fleet/announce. The parent router must apply admin auth.
func (h *FleetHandler) Register(r chi.Router) {
	r.Post("/fleet/announce", h.Announce)
}

// announceRequest is the heartbeat body Front Desk sends each member.
type announceRequest struct {
	IsPrimary   bool   `json:"is_primary"`
	PrimaryName string `json:"primary_name,omitempty"`
	FrontdeskID string `json:"frontdesk_id,omitempty"`
}

// Announce records a Front Desk contact. It writes only routing metadata
// (timestamps, a primary flag, display names) — never request content — to the
// instance-local _fleet_* settings, and returns 204. Writes go through Set so
// the (non-allowlisted) keys are accepted; the same property keeps them out of
// config-sync's declarative replace.
func (h *FleetHandler) Announce(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAnnounceBody)
	var req announceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	now := time.Now().UTC().Format(time.RFC3339)
	// One multi-row upsert, not four sequential writes: Front Desk's announce
	// client gives up after a few seconds, and four round-trips against a
	// briefly-slow database (e.g. during a simultaneous fleet rebuild) can blow
	// that budget and surface as a spurious 500 on the member.
	writes := [][2]string{
		{keyFleetManagedSeenAt, now},
		{keyFleetIsPrimary, boolStr(req.IsPrimary)},
		{keyFleetPrimaryName, req.PrimaryName},
		{keyFleetFrontdeskID, req.FrontdeskID},
	}
	if err := h.settings.SetMany(ctx, writes); err != nil {
		respondError(w, "failed to record fleet announce", err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
