package frontdesk

import "net/http"

// versionSkewMember is one member whose app version differs from the primary's.
type versionSkewMember struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	Version  string `json:"version"` // "" when unknown / unreadable
}

// versionCheckResponse reports the fleet's version alignment against a primary.
// An empty Skewed list means the fleet is safe to sync.
type versionCheckResponse struct {
	PrimaryID      string              `json:"primary_id"`
	PrimaryVersion string              `json:"primary_version"`
	Skewed         []versionSkewMember `json:"skewed"`
}

// fleetVersionCheck re-polls every tokened member's app_version on demand and
// reports the members that differ from the chosen primary's, so the Fleet Sync
// wizard can gate its sync step (and its Refresh button can clear the block
// right after the operator aligns a member, without waiting for the background
// poll interval). Comparison matches the sync gates: exact string, unknown
// versions fail closed and so count as skewed. It only reads versions and
// writes nothing to any member.
func (s *Server) fleetVersionCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrimaryID string `json:"primary_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	primary, _, err := s.memberTokenOrErr(ctx, req.PrimaryID)
	if err != nil {
		writeError(w, err)
		return
	}
	// Fresh read of the whole fleet's versions (bounded per-member probe timeout).
	s.poller.PollVersionsOnce(ctx)

	members, err := s.store.ListMembers(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	primaryVer := s.poller.MemberVersion(primary.ID)
	skewed := make([]versionSkewMember, 0)
	for _, m := range members {
		if m.ID == primary.ID || !m.HasToken {
			continue
		}
		mv := s.poller.MemberVersion(m.ID)
		if versionSkew(primaryVer, mv) {
			skewed = append(skewed, versionSkewMember{MemberID: m.ID, Name: m.Name, Version: mv})
		}
	}
	writeJSON(w, http.StatusOK, versionCheckResponse{
		PrimaryID: primary.ID, PrimaryVersion: primaryVer, Skewed: skewed,
	})
}
