package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
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
	// keyFleetActiveMembers is the fleet-wide count of StateActive members,
	// delivered by Front Desk's announce heartbeat. The rate limiters read it as a
	// fair-share divisor. Instance-local like the other _fleet_* keys: written via
	// Set/SetMany, so config-sync's declarative replace never touches it.
	keyFleetActiveMembers = "_fleet_active_members"
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
	// GetChecked distinguishes an unset key from a real read failure, so fleet
	// role decisions can refuse to guess "member" when the DB read errors.
	GetChecked(ctx context.Context, key string) (value string, found bool, err error)
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
// _fleet_* settings and the liveness windows. It returns (nil, nil) for a
// standalone instance (no recorded contact, or contact older than
// fleetForgetTTL) so the dashboard renders nothing. A non-nil error means a
// setting read failed: the caller must treat the fleet role as unknown and
// never fall back to "member" (a canceled /api/system read must not report the
// primary as a demoted member, nor poison the response cache). now is
// injectable for tests.
func computeFleetStatus(ctx context.Context, s fleetSettings, now time.Time) (*FleetStatus, error) {
	seen, found, err := s.GetChecked(ctx, keyFleetManagedSeenAt)
	if err != nil {
		return nil, err
	}
	if !found || seen == "" {
		return nil, nil // never contacted: standalone
	}
	seenAt, perr := time.Parse(time.RFC3339, seen)
	if perr != nil {
		// A corrupt stored timestamp is not a read failure: treat the instance as
		// standalone (unmanaged), never as a hard error. Returning (nil, nil) is
		// deliberate here, not a swallowed error.
		debuglog.Warn("fleet: unparseable managed-seen-at; treating as standalone", "value", seen)
		return nil, nil //nolint:nilerr // corrupt value means standalone, not error
	}
	age := now.Sub(seenAt)
	if age >= fleetForgetTTL {
		return nil, nil // long gone: forget the fleet, behave as standalone again
	}

	isPrimary, err := fleetGetOr(ctx, s, keyFleetIsPrimary, "false")
	if err != nil {
		return nil, err
	}
	primaryName, err := fleetGetOr(ctx, s, keyFleetPrimaryName, "")
	if err != nil {
		return nil, err
	}
	frontdeskID, err := fleetGetOr(ctx, s, keyFleetFrontdeskID, "")
	if err != nil {
		return nil, err
	}
	configSyncedAt, err := fleetGetOr(ctx, s, keyFleetConfigSyncedAt, "")
	if err != nil {
		return nil, err
	}

	fs := &FleetStatus{
		IsPrimary:      isPrimary == "true",
		PrimaryName:    primaryName,
		FrontdeskID:    frontdeskID,
		ManagedSeenAt:  seen,
		ConfigSyncedAt: configSyncedAt,
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
	return fs, nil
}

// fleetGetOr reads a fleet key via GetChecked, returning def when the key is
// unset and propagating any real read failure so the caller can refuse to guess.
func fleetGetOr(ctx context.Context, s fleetSettings, key, def string) (string, error) {
	v, found, err := s.GetChecked(ctx, key)
	if err != nil {
		return "", err
	}
	if !found {
		return def, nil
	}
	return v, nil
}

// FleetHandler serves POST /fleet/announce, the admin-authenticated heartbeat
// Front Desk pings on its poll. It is mounted inside the admin-authenticated
// /api group (see Handler.Register), so an unauthenticated caller cannot forge
// a member's fleet state.
type FleetHandler struct {
	settings fleetSettings

	// conflictMu guards conflictSeen, the per-rejected-ID debounce for the
	// conflict event + Warn. Announces from a losing Front Desk arrive every ~5s;
	// without this the member would emit ~720 events/hour per offending ID.
	conflictMu   sync.Mutex
	conflictSeen map[string]time.Time // rejected frontdesk_id -> last time we emitted
}

// conflictNotifyInterval bounds how often a rejected Front Desk's ownership
// conflict is surfaced (event + Warn) on the member. One per hour per ID is
// enough to make a persistent misconfiguration visible without flooding.
const conflictNotifyInterval = time.Hour

// NewFleetHandler builds the member-side fleet handler.
func NewFleetHandler(settings fleetSettings) *FleetHandler {
	return &FleetHandler{settings: settings, conflictSeen: map[string]time.Time{}}
}

// Register mounts POST /fleet/announce. The parent router must apply admin auth.
func (h *FleetHandler) Register(r chi.Router) {
	r.Post("/fleet/announce", h.Announce)
}

// announceRequest is the heartbeat body Front Desk sends each member.
type announceRequest struct {
	IsPrimary     bool   `json:"is_primary"`
	PrimaryName   string `json:"primary_name,omitempty"`
	FrontdeskID   string `json:"frontdesk_id,omitempty"`
	ActiveMembers int    `json:"active_members,omitempty"`
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

	// Every announce must identify its Front Desk. The id is how a member knows
	// which control plane owns it, so an anonymous announce cannot be trusted to
	// write fleet state at all: it would let anyone holding the admin token demote
	// a member out from under its real owner. A correctly-built Front Desk always
	// sends one (see store.EnsureFrontdeskID); an announce without it is rejected.
	if strings.TrimSpace(req.FrontdeskID) == "" {
		http.Error(w, "frontdesk_id is required", http.StatusBadRequest)
		return
	}

	// Ownership check: decide whether this announcer is allowed to write. A read
	// failure means unknown, so respond 500 and let the announcer retry in ~5s
	// rather than guessing. Evaluated BEFORE any write so a rejected announce
	// touches nothing.
	storedID, _, err := h.settings.GetChecked(ctx, keyFleetFrontdeskID)
	if err != nil {
		respondError(w, "failed to read fleet ownership", err, http.StatusInternalServerError)
		return
	}
	// Accept when there is no owner yet or the same owner is re-announcing. A
	// different owner is only displaced if its heartbeat has gone stale (it was
	// replaced); otherwise reject so a rogue second Front Desk cannot demote a
	// live member.
	if storedID != "" && storedID != req.FrontdeskID {
		seen, _, serr := h.settings.GetChecked(ctx, keyFleetManagedSeenAt)
		if serr != nil {
			respondError(w, "failed to read fleet heartbeat", serr, http.StatusInternalServerError)
			return
		}
		if !ownerStale(seen, time.Now()) {
			h.rejectConflict(ctx, storedID, req.FrontdeskID)
			http.Error(w, "another Front Desk currently manages this instance", http.StatusConflict)
			return
		}
		// Stale owner: legitimate FD-replacement path, adopt the new ID.
	}

	now := time.Now().UTC().Format(time.RFC3339)
	// One multi-row upsert, not sequential writes: Front Desk's announce client
	// gives up after a few seconds, and multiple round-trips against a briefly-slow
	// database (e.g. during a simultaneous fleet rebuild) can blow that budget and
	// surface as a spurious 500 on the member.
	writes := [][2]string{
		{keyFleetManagedSeenAt, now},
		{keyFleetIsPrimary, boolStr(req.IsPrimary)},
		{keyFleetPrimaryName, req.PrimaryName},
		{keyFleetFrontdeskID, req.FrontdeskID},
	}
	// A legacy Front Desk omits active_members (decodes to 0). Only record a real
	// count so an old control plane can never overwrite a live divisor with 0
	// (which the limiters read as "unlimited" — the wrong, unsafe direction).
	if req.ActiveMembers >= 1 {
		writes = append(writes, [2]string{keyFleetActiveMembers, strconv.Itoa(req.ActiveMembers)})
	}
	if err := h.settings.SetMany(ctx, writes); err != nil {
		respondError(w, "failed to record fleet announce", err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownerStale reports whether the current owner's last-heartbeat timestamp is
// older than fleetManagedTTL (or absent/unparseable), meaning the owning Front
// Desk is gone and its member may be adopted by a new one.
func ownerStale(seen string, now time.Time) bool {
	if seen == "" {
		return true
	}
	seenAt, err := time.Parse(time.RFC3339, seen)
	if err != nil {
		return true
	}
	return now.Sub(seenAt) >= fleetManagedTTL
}

// rejectConflict surfaces a rejected ownership claim: a debounced Warn plus a
// dashboard event (Events page + SSE), at most once per hour per rejected ID.
func (h *FleetHandler) rejectConflict(_ context.Context, storedID, rejectedID string) {
	now := time.Now()
	h.conflictMu.Lock()
	if h.conflictSeen == nil {
		h.conflictSeen = map[string]time.Time{}
	}
	last, seen := h.conflictSeen[rejectedID]
	shouldEmit := !seen || now.Sub(last) >= conflictNotifyInterval
	if shouldEmit {
		h.conflictSeen[rejectedID] = now
	}
	h.conflictMu.Unlock()
	if !shouldEmit {
		return
	}
	debuglog.Warn("fleet: rejected announce from unowned Front Desk",
		"stored_frontdesk_id", storedID, "rejected_frontdesk_id", rejectedID)
	events.Publish(events.Event{
		Type:     "fleet.conflict",
		Severity: "warning",
		Source:   "fleet",
		Message:  "Rejected a fleet announce from a Front Desk that does not own this instance",
		Metadata: map[string]any{
			"stored_frontdesk_id":   storedID,
			"rejected_frontdesk_id": rejectedID,
		},
	})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
