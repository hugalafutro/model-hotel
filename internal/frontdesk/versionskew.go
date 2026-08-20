package frontdesk

// unstampedCommit is what a binary built without the commit ldflag reports for
// app_commit (internal/api's buildCommit default). It names no build, so it
// cannot distinguish one.
const unstampedCommit = "unknown"

// memberBuild is what a member reports about the binary it runs: the app
// version and the source commit it was built from. Both arrive in one read of
// the member's settings API, and both are stamped at image build time
// (Dockerfile's -ldflags), so neither can drift from the running code.
type memberBuild struct {
	Version string
	Commit  string
}

// describe renders the build for an operator-facing event: the version alone
// when that is all there is, "dev (d18a96d1f84d)" when a commit backs it. An
// unknown build reads as "unknown" rather than an empty string, so a message
// naming it does not trail off.
func (b memberBuild) describe() string {
	switch {
	case b.Version == "":
		return "unknown"
	case stampedCommit(b.Commit):
		return b.Version + " (" + b.Commit + ")"
	default:
		return b.Version
	}
}

// stampedCommit reports whether a commit string names an actual build.
func stampedCommit(c string) bool {
	return c != "" && c != unstampedCommit
}

// buildSkew reports whether a member runs a different build than the primary.
//
// Two signals, consulted in order. A version we cannot read fails closed: we
// never overwrite a member whose build we cannot confirm. A version that
// differs is skew outright, which is the case the gate was built for — an older
// primary's export omits settings the newer member legitimately has, and the
// member-side converge-delete would drop them.
//
// The commit breaks the tie when the versions match, because on a self-built
// fleet they always match: every untagged image reports the "dev" placeholder
// (Dockerfile's ARG VERSION default), so string equality vouches for nothing.
// Without this, a rolling rebuild reads as an aligned fleet while its halves run
// different code, and the gate protects only the moments a member is unreachable
// enough to report no version at all.
//
// A commit only counts when both sides report a real one. A member built
// without the ldflag reports "unknown", and one too old to carry app_commit
// reports nothing; holding sync forever on a member that cannot answer would be
// a worse gate than the version-only one this replaces, so an unanswerable
// commit falls back to the version verdict rather than manufacturing skew.
func buildSkew(primary, member memberBuild) bool {
	if primary.Version == "" || member.Version == "" {
		return true
	}
	if primary.Version != member.Version {
		return true
	}
	if !stampedCommit(primary.Commit) || !stampedCommit(member.Commit) {
		return false
	}
	return primary.Commit != member.Commit
}
