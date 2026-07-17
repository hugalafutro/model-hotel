package frontdesk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	// Never expose the encrypted Apprise target or OIDC client secret: replace a
	// stored secret with a mask the UI can echo back unchanged to preserve it.
	if set.AlertAppriseTargets != "" {
		set.AlertAppriseTargets = alertMaskValue
	}
	if set.OidcClientSecret != "" {
		set.OidcClientSecret = alertMaskValue
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	// Partial-merge onto the current row: decode the request body on top of the
	// stored settings so a field the caller omits is preserved, not zeroed. The
	// polling form and the Alerts panel each PUT only the fields they own, so
	// neither clobbers the other; and an older client that never sends the alert
	// fields can no longer wipe the encrypted target. The target is presented as
	// the mask first, so an omitted or echoed value both resolve to "preserve"
	// (and the raw ciphertext is never re-encrypted into itself).
	//
	// The read-merge-write is serialized: two concurrent PUTs (e.g. both panels at
	// once) must not both read the same snapshot and have the later write restore
	// the other's pre-merge values. putSettings is the only writer of this row.
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if set.AlertAppriseTargets != "" {
		set.AlertAppriseTargets = alertMaskValue
	}
	if set.OidcClientSecret != "" {
		set.OidcClientSecret = alertMaskValue
	}
	if !decodeJSON(w, r, &set) {
		return
	}
	// Resolve the Apprise target secret before storing: a masked submission keeps
	// the existing ciphertext, a new value is encrypted at rest, a blank clears it.
	resolved, err := s.resolveAlertTarget(r.Context(), set.AlertAppriseTargets)
	if err != nil {
		writeError(w, err)
		return
	}
	set.AlertAppriseTargets = resolved
	// The OIDC client secret follows the same mask/encrypt/preserve contract.
	oidcSecret, err := s.resolveSecret(r.Context(), set.OidcClientSecret, func(cur Settings) string {
		return cur.OidcClientSecret
	})
	if err != nil {
		writeError(w, err)
		return
	}
	set.OidcClientSecret = oidcSecret

	if err := s.store.UpdateSettings(r.Context(), set); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "settings.changed", Severity: "info", Source: "frontdesk",
		Message: "Settings updated",
	})
	// Re-mask before echoing the saved settings back to the client.
	if set.AlertAppriseTargets != "" {
		set.AlertAppriseTargets = alertMaskValue
	}
	if set.OidcClientSecret != "" {
		set.OidcClientSecret = alertMaskValue
	}
	writeJSON(w, http.StatusOK, set)
}

// resolveSecret maps a submitted masked-secret field to the value to store: the
// mask sentinel preserves the existing stored ciphertext (read back via current),
// a blank clears it, and any other value is encrypted at rest with the Front Desk
// master key. Shared by every settings secret (Apprise target, OIDC client secret).
func (s *Server) resolveSecret(ctx context.Context, submitted string, current func(Settings) string) (string, error) {
	switch submitted {
	case alertMaskValue:
		cur, err := s.store.GetSettings(ctx)
		if err != nil {
			return "", err
		}
		return current(cur), nil
	case "":
		return "", nil
	default:
		return auth.EncryptString(submitted, s.masterKey)
	}
}

// resolveAlertTarget resolves the Apprise target secret (see resolveSecret).
func (s *Server) resolveAlertTarget(ctx context.Context, submitted string) (string, error) {
	return s.resolveSecret(ctx, submitted, func(cur Settings) string { return cur.AlertAppriseTargets })
}

// getAutoSync returns the automatic config-propagation setup (enabled + the
// designated primary). The internal drift hash is never included.
// autoSyncStatus is the GET/PUT /api/fleet/autosync body: the stored config plus
// a computed Stale flag. Stale is the same drift signal the poller alerts on
// (see autoSyncStale), surfaced here so a device that only polls this endpoint
// (Bellhop's background monitor) can raise its own notification without consuming
// the event stream. Embeds AutoSyncConfig so the wire shape stays a superset of
// the old one (enabled, primary_id, then stale).
type autoSyncStatus struct {
	AutoSyncConfig
	Stale bool `json:"stale"`
	// LastSyncAt is when any member's config was last actually written by a
	// sync (manual wizard run or the automatic loop): the max of the members'
	// last_config_sync_at stamps, which only move on a real write. Empty when
	// no sync has ever changed a member. Bellhop shows this under its
	// "Sync fleet from primary" action so the operator sees when the fleet
	// truly last synced, not when a button was last pressed.
	LastSyncAt string `json:"last_sync_at,omitempty"`
	// FleetState / FleetStateReasons are the fleet state machine's verdict
	// (fleetstate.go), ridden on this payload because it is the one endpoint
	// Bellhop's background monitor already polls. Reason codes are wire
	// constants the clients translate. Optional: an older client ignores them,
	// and an internal error omits them rather than failing the status read.
	FleetState        FleetState `json:"fleet_state,omitempty"`
	FleetStateReasons []string   `json:"fleet_state_reasons,omitempty"`
}

// autoSyncStatusNow reads the auto-sync config and last-sync marker and folds in
// the computed staleness, so getAutoSync and putAutoSync return an identical
// shape.
func (s *Server) autoSyncStatusNow(ctx context.Context) (autoSyncStatus, error) {
	cfg, err := s.store.GetAutoSync(ctx)
	if err != nil {
		return autoSyncStatus{}, err
	}
	state, found, err := s.store.GetFleetSyncState(ctx)
	if err != nil {
		return autoSyncStatus{}, err
	}
	status := autoSyncStatus{
		AutoSyncConfig: cfg,
		Stale:          autoSyncStale(cfg, state.LastRunAt, found, time.Now().UTC()),
	}
	// The member list feeds both the fleet-state fields and the last_sync_at
	// garnish, so it is read once here and reused for both (and both then see one
	// consistent snapshot). Best-effort, like Stale above: a failed read must not
	// fail the status endpoint (the PUT toggle already persisted by the time this
	// runs), so both derived fields degrade to absent instead.
	if members, err := s.store.ListMembers(ctx); err == nil {
		status.FleetState, status.FleetStateReasons = s.fleetStateFrom(ctx, members, cfg, state.LastRunAt, found)
		var lastSync time.Time
		for _, m := range members {
			if m.LastConfigSyncAt != nil && m.LastConfigSyncAt.After(lastSync) {
				lastSync = *m.LastConfigSyncAt
			}
		}
		if !lastSync.IsZero() {
			status.LastSyncAt = lastSync.UTC().Format(time.RFC3339Nano)
		}
	}
	return status, nil
}

func (s *Server) getAutoSync(w http.ResponseWriter, r *http.Request) {
	status, err := s.autoSyncStatusNow(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// putAutoSync sets the auto-sync toggle and designated primary. Enabling without
// a primary, or naming a primary that is unknown or has no stored admin token, is
// rejected: the loop could not authenticate to pull its config, so the choice
// would silently do nothing.
//
// Repointing or clearing an already-configured primary is high-impact (it changes
// which instance's config gets pushed across the whole fleet), so it is gated on a
// fresh admin-token confirmation. The bearer may be a passkey/TOTP session token
// rather than the raw FRONTDESK_TOKEN, so the check happens here against AdminMgr
// rather than client-side. The first primary selection (none configured yet) and
// changes that leave the primary untouched (e.g. just toggling enabled) need no
// confirmation.
// repointTargetsCurrentPrimary reports whether repointing the fleet primary to
// candidateID would land on the same physical host that is already the primary,
// reached under a different URL. It asks the candidate host's own HA self-report
// (see memberReportsPrimary), so the answer is independent of the member id or
// URL string. Returns false on the first designation (no current primary), on a
// same-member-row re-select (a no-op), and it fails open (false) when the
// candidate cannot be probed, since the admin-token gate still protects the
// repoint and blocking a legitimate change on a transient read is worse.
func (s *Server) repointTargetsCurrentPrimary(ctx context.Context, candidateID string) (bool, error) {
	cur, err := s.store.GetAutoSync(ctx)
	if err != nil {
		return false, err
	}
	if cur.PrimaryID == "" || cur.PrimaryID == candidateID {
		return false, nil
	}
	m, err := s.store.GetMember(ctx, candidateID)
	if err != nil {
		return false, err
	}
	token, ok, err := s.store.MemberToken(ctx, candidateID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	isPrimary, _, determined := s.memberIdentity(ctx, m.URL, token)
	return determined && isPrimary, nil
}

// instanceAlreadyMember reports whether instanceID belongs to a member other
// than excludeID. It compares against each member's stored instance_id; for a
// member whose identity is not yet known (empty stored id, e.g. added before
// instance identity existed) it probes /api/system once and backfills the
// learned id, so the check is correct without a separate migration pass. A
// member that cannot be probed is skipped (it simply cannot be deduped yet); a
// store read failure is surfaced so the caller can refuse rather than guess.
//
// Two simultaneous adds of the same physical instance under different URLs can
// both create a row before either records its instance_id, so each dedup pass
// sees the other and both roll back. That resolves toward the safe outcome
// (neither added, no duplicate persisted); the operator simply retries once.
func (s *Server) instanceAlreadyMember(ctx context.Context, excludeID, instanceID string) (bool, error) {
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range members {
		if m.ID == excludeID {
			continue
		}
		known := m.InstanceID
		if known == "" && m.HasToken {
			token, ok, terr := s.store.MemberToken(ctx, m.ID)
			if terr == nil && ok {
				if _, id, identOK := s.memberIdentity(ctx, m.URL, token); identOK && id != "" {
					known = id
					if serr := s.store.SetMemberInstanceID(ctx, m.ID, id); serr != nil {
						debuglog.Warn("frontdesk: could not backfill member instance id", "member", m.ID, "error", serr)
					}
				}
			}
		}
		if known == instanceID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) putAutoSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled      bool   `json:"enabled"`
		PrimaryID    string `json:"primary_id"`
		ConfirmToken string `json:"confirm_token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PrimaryID != "" {
		m, err := s.store.GetMember(r.Context(), req.PrimaryID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "that primary is not a known member", http.StatusBadRequest)
				return
			}
			writeError(w, err)
			return
		}
		if !m.HasToken {
			http.Error(w, "store an admin token for that primary first; auto-sync needs it to read the primary's config", http.StatusBadRequest)
			return
		}
	} else if req.Enabled {
		http.Error(w, "choose a primary before enabling auto-sync", http.StatusBadRequest)
		return
	}
	// Repointing or clearing an already-configured primary is high-impact (it
	// changes which instance's config the whole fleet copies), so it requires a
	// valid admin token. The bearer here may be a passkey/TOTP session token
	// rather than the raw FRONTDESK_TOKEN, so the operator re-supplies the token
	// in the body and we check it against AdminMgr. The guard is enforced inside
	// the same UPDATE that writes (SetAutoSyncGuarded), so there is no
	// read-modify-write window for a concurrent repoint to slip through.
	tokenValid := s.adminMgr.Validate(strings.TrimSpace(req.ConfirmToken))
	// Same-host guard: a valid token authorises changing the primary, but if the
	// newly-selected host is the same physical instance as the current primary
	// reached under a different URL (public DNS vs a LAN address), the "change" is
	// a no-op replacement of the source of truth with itself. Ask the candidate
	// host its own HA self-report (id/URL-independent) and refuse if it already is
	// the primary. Only checked on an authorised repoint, so no/wrong-token
	// attempts short-circuit at the token gate below without probing a member.
	if tokenValid && req.PrimaryID != "" {
		same, serr := s.repointTargetsCurrentPrimary(r.Context(), req.PrimaryID)
		if serr != nil {
			writeError(w, serr)
			return
		}
		if same {
			http.Error(w, "you cannot replace the primary with the same host: the selected host is already the fleet primary (reached under a different address)", http.StatusConflict)
			return
		}
	}
	applied, err := s.store.SetAutoSyncGuarded(r.Context(), req.Enabled, req.PrimaryID, tokenValid)
	if err != nil {
		writeError(w, err)
		return
	}
	if !applied {
		// The change would repoint/clear a configured primary without a valid
		// token (or lost a concurrent repoint). No secrets are logged, only that a
		// confirmation-required change was refused.
		debuglog.Warn("frontdesk: refused unconfirmed auto-sync primary change", "to", req.PrimaryID)
		http.Error(w, "confirm the admin token to change or clear the configured primary", http.StatusForbidden)
		return
	}
	// The guarded write bumped the generation: cancel any pass still importing the
	// old primary's config before the kick below starts a fresh pass.
	s.signalRearm()
	s.emit(r.Context(), Event{
		Type: "settings.changed", Severity: "info", Source: "frontdesk",
		Message: fmt.Sprintf("Auto-sync %s", enabledWord(req.Enabled)),
	})
	status, err := s.autoSyncStatusNow(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	// When auto-sync is left on, converge the fleet right away instead of waiting
	// up to two ticks for the loop: the operator opted in deliberately, so this is
	// the "sync now" they expect from the toggle. Detached from the request
	// context (which ends when we respond) but time-bounded so a stuck pass cannot
	// leak the goroutine. A no-op when nothing has drifted; the loop still owns the
	// steady-state watch. Disabling (or no primary) never kicks.
	if status.Enabled && status.PrimaryID != "" {
		kickCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), autoSyncKickTimeout)
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			defer cancel()
			s.forceAutoSyncNow(kickCtx)
		}()
	}
	writeJSON(w, http.StatusOK, status)
}

func enabledWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// rearmAutoSync clears the last-applied config hash so the auto-sync loop runs a
// full convergence pass on its next tick. The loop's fast path skips work when the
// primary's hash is unchanged, so a member that becomes newly syncable (just
// added, or just given an admin token) would otherwise stay stale until the
// primary's config next changed. Clearing the marker re-arms the loop; members
// already matching the primary are still skipped by their own dry-run diff, so the
// re-run only syncs the one(s) that actually need it. It is a no-op in effect when
// auto-sync is disabled (the loop reads the marker but does nothing).
func (s *Server) rearmAutoSync(ctx context.Context) {
	if err := s.store.RearmAutoSync(ctx); err != nil {
		debuglog.Warn("frontdesk: re-arm auto-sync after membership change", "error", err)
		return
	}
	s.signalRearm()
}

// signalRearm wakes every in-flight convergence pass so it cancels synchronously,
// the instant a rearm/repoint has bumped the auto-sync generation, rather than on
// the next poll. Call it only after the generation-bumping write has committed.
// Closing the channel broadcasts to all current waiters; a fresh channel replaces
// it for the next pass. watchRearm still confirms the generation actually moved
// before cancelling, so a spurious wake never aborts a valid pass.
func (s *Server) signalRearm() {
	s.rearmMu.Lock()
	close(s.rearmCh)
	s.rearmCh = make(chan struct{})
	s.rearmMu.Unlock()
}

// rearmChan returns the current rearm-broadcast channel for a pass to wait on. It
// must be captured before the pass reads the generation it guards, so a rearm
// landing in between still wakes the waiter (the channel is closed, not missed).
func (s *Server) rearmChan() <-chan struct{} {
	s.rearmMu.Lock()
	defer s.rearmMu.Unlock()
	return s.rearmCh
}
