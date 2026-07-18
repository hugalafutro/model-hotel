package frontdesk

import (
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
	m, err := s.store.CreateMember(r.Context(), req.Name, req.URL, req.Token)
	if err != nil {
		// Map the two validation failures the add form routes on to stable codes;
		// everything else falls back to the shared plain-text writeError.
		switch {
		case errors.Is(err, ErrDuplicateURL):
			writeCodedError(w, http.StatusBadRequest, "duplicate", err.Error())
		case errors.Is(err, ErrInsecureURL):
			writeCodedError(w, http.StatusBadRequest, "insecure_url", err.Error())
		default:
			writeError(w, err)
		}
		return
	}
	// rollback removes the just-created row when a verification step fails, so a
	// rejected add leaves no half-added member (and no duplicate-URL wall on retry).
	rollback := func(code, userMsg string, status int) {
		if delErr := s.store.DeleteMember(r.Context(), m.ID); delErr != nil {
			writeCodedError(w, http.StatusInternalServerError, "rollback_failed",
				fmt.Sprintf("%s Rolling back the add also failed (%v); remove it from the Members list and try again.", userMsg, delErr))
			return
		}
		writeCodedError(w, status, code, userMsg)
	}

	// Verify the token against the (now canonical) member URL. Unlike an edit, an
	// add requires a positive reply: an unreachable host or a refused/unexpected
	// response blocks the add rather than warning, so only live, verified members
	// enter the list.
	p := s.probeMemberToken(r.Context(), m.URL, req.Token)
	if !p.valid {
		switch {
		case !p.reached:
			rollback("unreachable", "Front Desk could not reach this member to verify it. Check the URL and that the host is running, then try again.", http.StatusBadRequest)
		case p.rejected():
			rollback("token_rejected", fmt.Sprintf("This member rejected the admin token (HTTP %d). Double-check the token and try again.", p.status), http.StatusBadRequest)
		default:
			rollback("unverified", fmt.Sprintf("This host did not verify as a Front Desk member (HTTP %d). Check the URL points at a model-hotel instance and the token is correct.", p.status), http.StatusBadRequest)
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
	isPrimary, instanceID, identOK := s.memberIdentity(r.Context(), m.URL, req.Token)
	if !identOK {
		rollback("identity_unverified", "Front Desk verified the admin token but could not read this host's fleet identity (/api/system) to confirm it is not the fleet primary or an existing member. Check the host and try again.", http.StatusBadRequest)
		return
	}
	// Reject the fleet primary re-added under a different URL. Only one primary
	// exists, so a host self-reporting is_primary is that primary reached under
	// another address.
	if isPrimary {
		rollback("already_primary", "This host is already the fleet primary (the config source of truth), reached under a different address. It cannot also be added as a member.", http.StatusConflict)
		return
	}
	// Reject a host that is already a member under a different URL: compare its
	// instance_id against every other member. Any member whose id we do not yet
	// know is probed once and backfilled, so this stays correct even for members
	// added before instance identity existed. A pre-056 host that exposes no
	// instance_id skips dedup (there is nothing to compare); it is the one
	// residual gap, and adds now require a token anyway.
	if instanceID != "" {
		dup, derr := s.instanceAlreadyMember(r.Context(), m.ID, instanceID)
		if derr != nil {
			rollback("verify_failed", "Front Desk could not verify whether this host is already a member. Try again.", http.StatusInternalServerError)
			return
		}
		if dup {
			rollback("already_member", "This host is already a member (added under a different address). Remove the existing entry first if you want to re-add it.", http.StatusConflict)
			return
		}
		// Persist the learned identity so future adds can dedup against this
		// member without re-probing it. A failure to record it would leave the
		// member half-registered (present but un-deduplicable), so roll the add
		// back rather than let a duplicate slip in under a different URL later.
		if err := s.store.SetMemberInstanceID(r.Context(), m.ID, instanceID); err != nil {
			debuglog.Warn("frontdesk: could not store member instance id", "member", m.ID, "error", err)
			rollback("verify_failed", "Front Desk verified this host but could not record its identity. Try again.", http.StatusInternalServerError)
			return
		}
	}

	// A newly added member with a valid token is stale relative to the primary;
	// re-arm auto-sync so the next tick brings it in line (no-op when disabled).
	s.rearmAutoSync(r.Context())
	s.emit(r.Context(), Event{
		Type: "member.added", Severity: "info", Source: "frontdesk",
		Message: m.Name + " added", MemberID: m.ID,
		Metadata: map[string]any{"url": m.URL},
	})
	writeJSON(w, http.StatusCreated, memberResponse{Member: m})
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
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	// The fleet primary is the config source of truth and cannot be removed here
	// at all: changing it goes through the Fleet Sync wizard (a token-gated
	// repoint). The primary-status check and the delete run as one atomic SQL
	// statement inside DeleteMemberIfNotPrimary, so a concurrent repoint cannot
	// race past the check.
	applied, err := s.store.DeleteMemberIfNotPrimary(r.Context(), id)
	if err != nil {
		// Removing the last active member would empty the routing pool; refuse with
		// the same stable code the drain guard uses (drain first is not enough here:
		// the member must first be reactivated elsewhere or another member added).
		if errors.Is(err, ErrLastActiveMember) {
			writeCodedError(w, http.StatusConflict, "last_active_member",
				"cannot remove the last active member: the fleet would have no routable backends")
			return
		}
		writeError(w, err)
		return
	}
	if !applied {
		http.Error(w, "this host is the fleet primary (the config source of truth); change the primary from the Fleet Sync wizard before removing it", http.StatusConflict)
		return
	}
	s.emit(r.Context(), Event{
		Type: "member.removed", Severity: "info", Source: "frontdesk",
		Message: m.Name + " removed", MemberID: m.ID,
	})
	w.WriteHeader(http.StatusNoContent)
}

type memberStateRequest struct {
	State MemberState `json:"state"`
}

func (s *Server) setMemberState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req memberStateRequest
	if !decodeJSON(w, r, &req) {
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
	severity := "info"
	if req.State == StateDrained {
		severity = "warning"
	}
	s.emit(r.Context(), Event{
		Type: "member.state_changed", Severity: severity, Source: "frontdesk",
		Message: m.Name + " set to " + string(req.State), MemberID: m.ID,
		Metadata: map[string]any{"state": string(req.State), "initiated_by": actorFromContext(r.Context())},
	})
	writeJSON(w, http.StatusOK, m)
}
