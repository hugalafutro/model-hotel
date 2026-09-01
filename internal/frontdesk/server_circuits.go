package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// The fleet-wide circuit-breaker reset: the operation the 2026-08-31 reset
// loop performed by hand, member by member. Each member's breaker is local
// runtime state that nothing syncs, so the only way to clear a group's circuits
// across the fleet is to ask every member. Front Desk already holds the member
// tokens for config sync; this reuses that path.

// fleetCircuitResetRequest names the failover group whose entries to clear on
// every member. An empty group_id clears every circuit on every member.
type fleetCircuitResetRequest struct {
	GroupID string `json:"group_id"`
}

// fleetCircuitResetMember is one member's outcome. Cleared and Recovered are
// the member's own counts (circuits, not providers); Error is set when the
// member could not be reached or answered anything but 200.
type fleetCircuitResetMember struct {
	MemberID  string `json:"member_id"`
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Cleared   int    `json:"cleared"`
	Recovered int    `json:"recovered"`
	Error     string `json:"error,omitempty"`
}

type fleetCircuitResetResponse struct {
	GroupID   string                    `json:"group_id,omitempty"`
	Members   []fleetCircuitResetMember `json:"members"`
	Cleared   int                       `json:"cleared"`
	Recovered int                       `json:"recovered"`
	Failed    int                       `json:"failed"`
}

// memberCircuitResetResult is the shape both member endpoints answer with;
// the group variant carries more, but these two are all the fleet total needs.
type memberCircuitResetResult struct {
	Cleared   int `json:"cleared"`
	Recovered int `json:"recovered"`
}

// fleetCircuitReset fans a circuit-breaker reset out to every member that has
// a stored token, in sequence (a handful of members, one small POST each), and
// reports per member. A member that fails does not stop the others: the
// operator wants the fleet cleared, and the response says who was not.
func (s *Server) fleetCircuitReset(w http.ResponseWriter, r *http.Request) {
	var req fleetCircuitResetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GroupID != "" {
		if _, err := uuid.Parse(req.GroupID); err != nil {
			writeCodedError(w, http.StatusBadRequest, "invalid_group_id", "group_id must be a UUID")
			return
		}
	}
	ctx := r.Context()
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	resp := fleetCircuitResetResponse{GroupID: req.GroupID, Members: make([]fleetCircuitResetMember, 0, len(members))}
	for _, m := range members {
		if !m.HasToken {
			continue
		}
		res := s.resetMemberCircuits(ctx, m, req.GroupID)
		resp.Members = append(resp.Members, res)
		if !res.OK {
			resp.Failed++
			continue
		}
		resp.Cleared += res.Cleared
		resp.Recovered += res.Recovered
	}

	scope := "every circuit"
	if req.GroupID != "" {
		scope = "failover group " + req.GroupID
	}
	severity := "info"
	if resp.Failed > 0 {
		severity = "warning"
	}
	s.emit(ctx, Event{
		Type: "fleet.circuit_breaker_reset", Severity: severity, Source: "frontdesk",
		Message: fmt.Sprintf("circuit breakers reset on %d of %d members (%s): %d circuits cleared, %d recovered",
			len(resp.Members)-resp.Failed, len(resp.Members), scope, resp.Cleared, resp.Recovered),
		Metadata: map[string]any{
			"group_id": req.GroupID, "members": len(resp.Members), "failed": resp.Failed,
			"cleared": resp.Cleared, "recovered": resp.Recovered, "initiated_by": actorFromContext(ctx),
		},
	})
	writeJSON(w, http.StatusOK, resp)
}

// resetMemberCircuits performs one member's reset: the group's circuits when a
// group is named, else the whole ledger. The member's counts are parsed from
// its own response; any transport error or non-200 is reported, never hidden.
func (s *Server) resetMemberCircuits(ctx context.Context, m *Member, groupID string) fleetCircuitResetMember {
	res := fleetCircuitResetMember{MemberID: m.ID, Name: m.Name}
	token, ok, err := s.store.MemberToken(ctx, m.ID)
	if err != nil || !ok {
		res.Error = "no stored admin token"
		return res
	}
	path := "/api/failover-groups/circuit-breaker/reset"
	if groupID != "" {
		path = "/api/failover-groups/" + groupID + "/circuit-breaker/reset"
	}
	status, body, err := s.callMember(ctx, http.MethodPost, m.URL, path, token, nil)
	if err != nil {
		res.Error = "could not reach this member"
		return res
	}
	if status != http.StatusOK {
		res.Error = fmt.Sprintf("member answered %d", status)
		return res
	}
	var counts memberCircuitResetResult
	if err := json.Unmarshal(body, &counts); err != nil {
		res.Error = "member answered with an unreadable body"
		return res
	}
	res.OK = true
	res.Cleared = counts.Cleared
	res.Recovered = counts.Recovered
	return res
}

// fleetFailoverGroup is one row of the primary's failover-group list as the
// Members page needs it to scope a fleet-wide circuit reset: the id the reset
// takes, and the names an operator recognises.
type fleetFailoverGroup struct {
	ID           string  `json:"id"`
	DisplayModel string  `json:"display_model"`
	DisplayName  *string `json:"display_name,omitempty"`
	Entries      int     `json:"entries"`
	GroupEnabled bool    `json:"group_enabled"`
}

// fleetFailoverGroups relays the primary's GET /api/failover-groups, reduced to
// what a group picker needs. Groups are synced config, so the primary's list is
// the fleet's list; asking every member would only repeat it. primary_id is
// required because Front Desk holds no group knowledge of its own.
func (s *Server) fleetFailoverGroups(w http.ResponseWriter, r *http.Request) {
	primaryID := r.URL.Query().Get("primary_id")
	if primaryID == "" {
		writeCodedError(w, http.StatusBadRequest, "primary_required", "primary_id is required")
		return
	}
	primary, token, err := s.memberTokenOrErr(r.Context(), primaryID)
	if err != nil {
		writeError(w, err)
		return
	}
	status, body, err := s.callMember(r.Context(), http.MethodGet, primary.URL, "/api/failover-groups", token, nil)
	if err != nil {
		writeCodedError(w, http.StatusBadGateway, "primary_unreachable", "could not reach the primary member")
		return
	}
	if status != http.StatusOK {
		writeCodedError(w, http.StatusBadGateway, "primary_error", fmt.Sprintf("the primary answered %d", status))
		return
	}
	var raw []struct {
		ID           string  `json:"id"`
		DisplayModel string  `json:"display_model"`
		DisplayName  *string `json:"display_name"`
		GroupEnabled bool    `json:"group_enabled"`
		Entries      []any   `json:"entries"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		writeCodedError(w, http.StatusBadGateway, "primary_error", "the primary answered with an unreadable body")
		return
	}
	out := make([]fleetFailoverGroup, 0, len(raw))
	for _, g := range raw {
		out = append(out, fleetFailoverGroup{ID: g.ID, DisplayModel: g.DisplayModel, DisplayName: g.DisplayName, Entries: len(g.Entries), GroupEnabled: g.GroupEnabled})
	}
	writeJSON(w, http.StatusOK, out)
}
