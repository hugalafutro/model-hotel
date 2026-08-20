package frontdesk

import "net/http"

// versionSkewMember is one member whose build differs from the primary's.
type versionSkewMember struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	Version  string `json:"version"` // "" when unknown / unreadable
	Commit   string `json:"commit"`  // "" when unknown, or the member predates app_commit
}

// versionCheckResponse reports the fleet's build alignment against a primary.
// An empty Skewed list means the fleet is safe to sync.
//
// PrimaryCommit is what lets the wizard tell an aligned fleet from an
// unvouchable one: with every image reporting the "dev" placeholder version,
// only a commit says the builds actually match, and its absence is what the
// dev-fleet acknowledgment exists to cover.
type versionCheckResponse struct {
	PrimaryID      string              `json:"primary_id"`
	PrimaryVersion string              `json:"primary_version"`
	PrimaryCommit  string              `json:"primary_commit"`
	Skewed         []versionSkewMember `json:"skewed"`
	// CommitVouched is true when a real commit backed every alignment verdict in
	// this check: the primary reports one, and so does every member that came
	// back aligned. False means at least one verdict rested on the version alone,
	// which on a "dev" fleet means it rested on nothing — the case the wizard's
	// acknowledgment covers. An aligned fleet that IS commit-vouched needs no
	// acknowledgment, because the builds were compared, not assumed.
	CommitVouched bool `json:"commit_vouched"`
}

// fleetVersionCheck re-polls every tokened member's build on demand and reports
// the members that differ from the chosen primary's, so the Fleet Sync wizard
// can gate its sync step (and its Refresh button can clear the block right
// after the operator aligns a member, without waiting for the background poll
// interval). Comparison matches the sync gates exactly, buildSkew included, so
// the wizard cannot bless a fleet auto-sync will then hold. It only reads
// builds and writes nothing to any member.
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
	primaryBuild := s.poller.memberBuildOf(primary.ID)
	skewed := make([]versionSkewMember, 0)
	vouched := stampedCommit(primaryBuild.Commit)
	for _, m := range members {
		if m.ID == primary.ID || !m.HasToken {
			continue
		}
		mb := s.poller.memberBuildOf(m.ID)
		if buildSkew(primaryBuild, mb) {
			skewed = append(skewed, versionSkewMember{
				MemberID: m.ID, Name: m.Name, Version: mb.Version, Commit: mb.Commit,
			})
			continue
		}
		// An aligned member that named no commit was aligned on its version alone,
		// so nothing vouched for its build. One such member unvouches the check:
		// the operator is being asked to bless the whole fleet, not a subset.
		if !stampedCommit(mb.Commit) {
			vouched = false
		}
	}
	writeJSON(w, http.StatusOK, versionCheckResponse{
		PrimaryID: primary.ID, PrimaryVersion: primaryBuild.Version,
		PrimaryCommit: primaryBuild.Commit, Skewed: skewed, CommitVouched: vouched,
	})
}
