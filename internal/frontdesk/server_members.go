package frontdesk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

// memberView is a member plus its live poller status for the Members tab.
type memberView struct {
	*Member
	Status MemberStatus `json:"status"`
	// NewestEvent is this member's most recent member-scoped event, attached so a
	// monitor client renders its latest-event pill without a per-member events
	// fetch. Omitted when the member has no events yet.
	NewestEvent *Event `json:"newest_event,omitempty"`
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	snap := s.poller.Snapshot()
	// Each member's newest event is read in one grouped query and attached inline
	// so a monitor client can render every card's latest-event pill without a
	// per-member events fetch. This read is best-effort: a failure must not fail
	// the members list, so it degrades to no inline pills (clients then fall back
	// to their own per-member fetch) rather than 500ing the whole tab.
	newest, err := s.store.NewestEventPerMember(r.Context())
	if err != nil {
		debuglog.Warn("frontdesk: could not read newest events for members list", "error", err)
		newest = nil
	}
	views := make([]memberView, len(members))
	for i, m := range members {
		// This list is monitor-readable: a row stored before normalizeMemberURL
		// began rejecting userinfo must not surface its credentials. The store
		// row keeps the URL as written; only the rendered copy is stripped.
		if stripped := stripUserinfo(m.URL); stripped != m.URL {
			mc := *m
			mc.URL = stripped
			m = &mc
		}
		views[i] = memberView{Member: m, Status: snap[m.ID]}
		if e, ok := newest[m.ID]; ok {
			ev := e
			views[i].NewestEvent = &ev
		}
	}
	writeJSON(w, http.StatusOK, views)
}

type createMemberRequest struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
	// ConfirmToken is this Front Desk's own admin token, re-supplied to enrol a
	// host that still names ANOTHER Front Desk as the owner of its primary role
	// (see the primary_elsewhere refusal below). It is the operator stating that
	// the other desk is really gone - typically because this one replaced it and
	// was rebuilt from an empty database, so it no longer carries the id the
	// member remembers. The bearer on the request may be a passkey or TOTP
	// session rather than the raw token, which is why the token is asked for
	// again here rather than inferred from being signed in, exactly as the
	// primary repoint does.
	ConfirmToken string `json:"confirm_token"`
}

// memberResponse is a Member plus an optional, non-fatal warning surfaced after
// an add/edit when the admin token could not be confirmed (the member was
// offline, or answered without a 200). The frontend toasts token_warning when
// it is present; a token the member positively refused is a 400 instead.
type memberResponse struct {
	*Member
	TokenWarning string `json:"token_warning,omitempty"`
}

func (s *Server) createMember(w http.ResponseWriter, r *http.Request) {
	var req createMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	// A member is only added once it replies and verifies: the admin token is
	// required (there is no way to confirm the host's identity or fleet role
	// without it), the host must answer the authenticated probe, and it must not
	// already be the fleet primary reached under a different URL. This keeps the
	// member list to genuine, distinct model-hotel instances and stops the primary
	// (the config source of truth) from being re-added as an ordinary member.
	if strings.TrimSpace(req.Token) == "" {
		writeCodedError(w, http.StatusBadRequest, "token_required",
			"an admin token is required to add a member: Front Desk uses it to verify the host and confirm its fleet role before adding it")
		return
	}
	// Validated and canonicalised, but not yet inserted: the host is verified
	// first, so a rejected add never leaves a row behind, not even for the
	// seconds the probes take (a row that exists that long counts toward the
	// fleet-size floor a concurrent removal checks).
	name, memberURL, err := s.store.ValidateMember(r.Context(), req.Name, req.URL)
	if err != nil {
		writeMemberValidationError(w, err)
		return
	}
	fail := func(code, userMsg string, status int) {
		writeCodedError(w, status, code, userMsg)
	}

	// Verify the token against the canonical member URL. Unlike an edit, an
	// add requires a positive reply: an unreachable host or a refused/unexpected
	// response blocks the add rather than warning, so only live, verified members
	// enter the list.
	p := s.probeMemberToken(r.Context(), memberURL, req.Token)
	if !p.valid {
		switch {
		case !p.reached:
			fail("unreachable", "Front Desk could not reach this member to verify it. Check the URL and that the host is running, then try again.", http.StatusBadRequest)
		case p.rejected():
			fail("token_rejected", fmt.Sprintf("This member rejected the admin token (HTTP %d). Double-check the token and try again.", p.status), http.StatusBadRequest)
		default:
			fail("unverified", fmt.Sprintf("This host did not verify as a Front Desk member (HTTP %d). Check the URL points at a model-hotel instance and the token is correct.", p.status), http.StatusBadRequest)
		}
		return
	}
	// Read the host's identity once: whether it self-reports as the fleet primary,
	// and its stable instance_id. Both are URL-independent, so they catch the same
	// physical instance reached under a different address. The token probe above
	// already confirmed the host is live and genuine, so a failure to read its
	// identity here is anomalous: rather than fail open (which could admit the
	// primary or a duplicate under a new URL), block the add and let the operator
	// retry once the host answers /api/system cleanly.
	ident, identOK := s.memberIdentity(r.Context(), memberURL, req.Token)
	if !identOK {
		fail("identity_unverified", "Front Desk verified the admin token but could not read this host's fleet identity (/api/system) to confirm it is not the fleet primary or an existing member. Check the host and try again.", http.StatusBadRequest)
		return
	}
	// Only a host claiming the primary role needs this desk's own id, to tell
	// whose role it is.
	ownID := ""
	if ident.State == "primary" || ident.IsPrimary {
		id, idErr := s.store.EnsureFrontdeskID(r.Context())
		if idErr != nil {
			fail("identity_unverified", "Front Desk could not read its own fleet identity to check who manages this host. Try again.", http.StatusInternalServerError)
			return
		}
		ownID = id
	}
	// Reject a live fleet primary re-added under a different URL: a host whose
	// state is "primary" was announced to as the primary within the last 90
	// seconds. The exception is a host naming THIS desk: removing it or
	// disbanding announces nothing, so it keeps reporting the live state for up
	// to 90s after this desk dropped it, and an immediate re-add is exactly what
	// the operator means (the stale own-desk case below). Such a host falls
	// through to the instance_id dedup, which refuses it as already_member when
	// it is in fact still in the roster under another address. Without an
	// instance id that dedup cannot run, so the refusal stands.
	if ident.State == "primary" && (ident.FrontdeskID != ownID || ident.InstanceID == "") {
		fail("already_primary", "This host is already the fleet primary (the config source of truth), reached under a different address. It cannot also be added as a member.", http.StatusConflict)
		return
	}
	// Past that window the host still reports the role it last heard (the flag
	// outlives the fleet by 24h), and only the Front Desk id it names separates
	// the two reasons the announces stopped.
	//
	// Another desk's id: that desk may simply be unreachable right now, and
	// enrolling its primary here would adopt it out from under a live fleet the
	// moment this desk announced (the member accepts a new owner once the old
	// one's heartbeat is stale). Refuse; waiting it out is recoverable, a
	// silent ownership transfer is not.
	//
	// Our own id: this desk is the one that stopped announcing, i.e. the member
	// was removed or the fleet disbanded (which drops every row at once, leaving
	// nothing to announce the demotion). Re-adding it is exactly what the
	// operator means to do, so let it through instead of making them wait out a
	// role that only this desk could have given it.
	//
	// No id at all is treated as another desk's: only a member too old to report
	// one answers that way, and refusing it is what this check has always done.
	//
	// The other desk may also be gone for good rather than briefly away: it was
	// replaced by this one, or rebuilt from an empty database and so no longer
	// carries the id the member remembers. There is no way to tell from here, and
	// waiting the host out takes until its role expires (fleetForgetTTL, 24h), so
	// the operator settles it by re-supplying this Front Desk's admin token. That
	// keeps the refusal in front of an accidental takeover while leaving a
	// deliberate recovery one confirmed step away, and the takeover is logged and
	// carried on the member.added event so it is never silent.
	takenOver := false
	if ident.IsPrimary && ident.FrontdeskID != ownID {
		if !s.adminMgr.Validate(strings.TrimSpace(req.ConfirmToken)) {
			fail("primary_elsewhere", "Another Front Desk still names this host its fleet primary (the config source of truth). It may only be unreachable right now, and adding it here would take its fleet over. Remove it there first, or, if that Front Desk is gone for good, confirm this Front Desk's admin token to enrol it anyway.", http.StatusConflict)
			return
		}
		takenOver = true
		debuglog.Warn("frontdesk: confirmed enrolment of another Front Desk's primary",
			"url", stripUserinfo(memberURL), "instance_id", ident.InstanceID, "frontdesk_id", ident.FrontdeskID)
	}
	instanceID := ident.InstanceID
	// Reject a host that is already a member under a different URL: compare its
	// instance_id against every other member. Any member whose id we do not yet
	// know is probed once and backfilled, so this stays correct even for members
	// added before instance identity existed. A pre-056 host that exposes no
	// instance_id skips dedup (there is nothing to compare); it is the one
	// residual gap, and adds now require a token anyway.
	if instanceID != "" {
		dup, derr := s.instanceAlreadyMember(r.Context(), "", instanceID)
		if derr != nil {
			fail("verify_failed", "Front Desk could not verify whether this host is already a member. Try again.", http.StatusInternalServerError)
			return
		}
		if dup {
			fail("already_member", "This host is already a member (added under a different address). Remove the existing entry first if you want to re-add it.", http.StatusConflict)
			return
		}
	}

	// Verified: insert, with the learned identity in the same statement so
	// future adds dedup against this member without re-probing it. The unique
	// indexes re-check both the URL and the instance id, so two adds racing on
	// the same host (same URL, or the same instance under two URLs, which the
	// scan above cannot catch while neither row exists) still end with one row.
	if err := s.recordLonePrimary(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	m, err := s.store.CreateVerifiedMember(r.Context(), name, memberURL, req.Token, instanceID)
	if err != nil {
		writeMemberValidationError(w, err)
		return
	}

	// A newly added member with a valid token is stale relative to the primary;
	// re-arm auto-sync so the next tick brings it in line (no-op when disabled).
	s.rearmAutoSync(r.Context())
	addedMetadata := map[string]any{"url": stripUserinfo(m.URL)}
	if takenOver {
		// The foreign desk's id; empty for a host too old to report one.
		addedMetadata["taken_over_from"] = ident.FrontdeskID
	}
	s.emit(r.Context(), Event{
		Type: "member.added", Severity: "info", Source: "frontdesk",
		Message: m.Name + " added", MemberID: m.ID,
		Metadata: addedMetadata,
	})
	writeJSON(w, http.StatusCreated, memberResponse{Member: m})
}

// recordLonePrimary keeps a one-member fleet's primary when a second member
// joins. On one member effectivePrimaryID names the sole member with nothing
// stored behind it, so without a record the two-member roster would resolve to
// nobody: the former lone member would be announced as a managed member, and
// the config it held while alone would be overwritten unannounced if the wizard
// then picked the newcomer. Recording it as the sync-state marker keeps the
// resolver naming it, and the wizard preselecting it, until the operator
// designates a primary. It runs before the insert, so the first announce that
// sees two rows already sees the marker; a failed insert leaves a marker that
// names the same member the one-member rule already does.
func (s *Server) recordLonePrimary(ctx context.Context) error {
	_, members, _, err := s.effectivePrimary(ctx)
	if err != nil || len(members) != 1 {
		return err
	}
	return s.store.SetFleetPrimaryMarker(ctx, members[0].ID, members[0].Name)
}

// writeMemberValidationError maps the two validation failures the add form
// routes on to stable codes; everything else falls back to the shared
// plain-text writeError.
func writeMemberValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDuplicateURL):
		writeCodedError(w, http.StatusBadRequest, "duplicate", err.Error())
	case errors.Is(err, ErrDuplicateInstance):
		writeCodedError(w, http.StatusConflict, "already_member", "This host is already a member (added under a different address). Remove the existing entry first if you want to re-add it.")
	case errors.Is(err, ErrInsecureURL):
		writeCodedError(w, http.StatusBadRequest, "insecure_url", err.Error())
	default:
		writeError(w, err)
	}
}

type patchMemberRequest struct {
	Name  *string `json:"name,omitempty"`
	Token *string `json:"token,omitempty"` // "" clears the stored token
}

func (s *Server) patchMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req patchMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		if err := s.store.RenameMember(r.Context(), id, *req.Name); err != nil {
			writeError(w, err)
			return
		}
	}
	var tokenWarning string
	if req.Token != nil {
		// Verify a non-empty new token before storing it, so a refused token is
		// rejected now rather than persisted. Clearing the token ("") never probes.
		if *req.Token != "" {
			m0, err := s.store.GetMember(r.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			p := s.probeMemberToken(r.Context(), m0.URL, *req.Token)
			if p.rejected() {
				http.Error(w, fmt.Sprintf("This member rejected the admin token (HTTP %d). Double-check the token and try again.", p.status), http.StatusBadRequest)
				return
			}
			tokenWarning = p.warning()
		}
		if err := s.store.SetMemberToken(r.Context(), id, *req.Token); err != nil {
			writeError(w, err)
			return
		}
		if *req.Token != "" {
			// The member just gained an admin token: it is now syncable but stale, so
			// re-arm auto-sync to converge it (no-op when disabled).
			s.rearmAutoSync(r.Context())
		}
	}
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memberResponse{Member: m, TokenWarning: tokenWarning})
}

func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// The fleet primary is the config source of truth and cannot be removed here
	// at all: changing it goes through the Fleet Sync wizard (a token-gated
	// repoint). A fleet is also never allowed to shrink to a single member:
	// removing a member from a two-member fleet disbands the whole fleet, primary
	// included (the UI warns before this call). Every guard runs inside the
	// delete statement itself, so a concurrent repoint cannot race past it.
	outcome, removed, err := s.store.DeleteMemberOrDisband(r.Context(), id)
	if err != nil {
		// Removing the last active member of a 3+ fleet would empty the routing
		// pool; refuse with the same stable code the drain guard uses (drain
		// first is not enough here: the member must first be reactivated
		// elsewhere or another member added).
		if errors.Is(err, ErrLastActiveMember) {
			writeCodedError(w, http.StatusConflict, "last_active_member",
				"cannot remove the last active member: the fleet would have no routable backends")
			return
		}
		// The roster changed under the operator's confirmed action (a concurrent
		// add or removal): what would happen now (plain removal vs disband) may
		// not be what the confirm described, so refuse and have them look again.
		if errors.Is(err, ErrMembershipChanged) {
			writeCodedError(w, http.StatusConflict, "membership_changed",
				"the fleet membership changed while removing this member; review the updated list and retry")
			return
		}
		writeError(w, err)
		return
	}
	// The removed roster names the target as the store saw it under the delete's
	// own transaction, so a rename racing the removal cannot make the
	// announcement quote a stale name.
	var targetName string
	for _, rm := range removed {
		if rm.ID == id {
			targetName = rm.Name
		}
	}
	switch outcome {
	case DeleteRefusedPrimary:
		http.Error(w, "this host is the fleet primary (the config source of truth); change the primary from the Fleet Sync wizard before removing it", http.StatusConflict)
		return
	case DeleteDisbanded:
		// Cancel any auto-sync pass still importing from the now-cleared primary
		// before announcing; the loop itself sees auto-sync disabled next tick.
		s.signalRearm()
		names := make([]string, 0, len(removed))
		for _, rm := range removed {
			s.forgetMemberState(rm.ID)
			names = append(names, rm.Name)
		}
		s.emit(r.Context(), Event{
			Type: "fleet.disbanded", Severity: "warning", Source: "frontdesk",
			Message: fmt.Sprintf("%s removed; fleet disbanded (a fleet cannot have fewer than two members): released %s",
				targetName, strings.Join(names, ", ")),
			MemberID: id,
		})
	case DeleteApplied:
		s.forgetMemberState(id)
		s.emit(r.Context(), Event{
			Type: "member.removed", Severity: "info", Source: "frontdesk",
			Message: targetName + " removed", MemberID: id,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// forgetMemberState drops the in-memory per-member state Front Desk keeps outside
// the store: the version-skew hold, the config divergence, the unconfirmed-push
// hash, and the backup staleness flag. All are read against the live member
// list, so this is hygiene rather than correctness: a re-added member starts
// clean, and the maps do not grow with every member ever removed.
func (s *Server) forgetMemberState(id string) {
	s.syncHeldMu.Lock()
	delete(s.syncHeld, id)
	delete(s.holdLogChecked, id)
	s.syncHeldMu.Unlock()

	s.syncIncompleteMu.Lock()
	delete(s.syncIncomplete, id)
	delete(s.unconfirmedSync, id)
	s.syncIncompleteMu.Unlock()

	s.backupStaleMu.Lock()
	delete(s.backupStale, id)
	s.backupStaleMu.Unlock()
}

type memberStateRequest struct {
	State MemberState `json:"state"`
	// Reason is why the state changes. "maintenance" marks a planned drain (the
	// fleet rebuild tool pulls a member before recreating it and puts it back
	// after), which is recorded as health.maintenance instead of
	// member.state_changed, so a picker that pages on that row stays quiet for
	// planned flips (the fleet-state row still notes the pool shrinking, since
	// it did). Empty is recorded as member.state_changed, as before.
	Reason string `json:"reason"`
}

// stateReasonMaintenance is the one reason a state change may carry.
const stateReasonMaintenance = "maintenance"

func (s *Server) setMemberState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req memberStateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Reason != "" && req.Reason != stateReasonMaintenance {
		writeCodedError(w, http.StatusBadRequest, "invalid_reason",
			"reason must be omitted or \"maintenance\"")
		return
	}
	if err := s.store.SetMemberState(r.Context(), id, req.State); err != nil {
		// Draining the last active member would empty the routing pool; refuse with
		// a stable code so the client can translate rather than match English.
		if errors.Is(err, ErrLastActiveMember) {
			writeCodedError(w, http.StatusConflict, "last_active_member",
				"cannot drain the last active member: the fleet would have no routable backends")
			return
		}
		writeError(w, err)
		return
	}
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	ev := Event{
		Type: "member.state_changed", Severity: "info", Source: "frontdesk",
		Message: m.Name + " set to " + string(req.State), MemberID: m.ID,
		Metadata: map[string]any{"state": string(req.State), "initiated_by": actorFromContext(r.Context())},
	}
	switch {
	case req.Reason == stateReasonMaintenance:
		// A planned drain is a maintenance note, like the health flips the
		// poller records for a drained member, so the same type carries it.
		// Both halves stay at info: this is the operator's action, not an
		// observed recovery, and the poller's own "maintenance over" note
		// reports the member answering again.
		ev.Type = "health.maintenance"
		ev.Message += " for maintenance"
		ev.Metadata["reason"] = stateReasonMaintenance
	case req.State == StateDrained:
		ev.Severity = "warning"
	}
	s.emit(r.Context(), ev)
	writeJSON(w, http.StatusOK, m)
}
