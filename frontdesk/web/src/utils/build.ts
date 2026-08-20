// A member's build identity: the app version it reports plus the commit that
// version was built from. Both are stamped into the binary at image build time
// (the Dockerfile's -ldflags), so neither can drift from the running code.
export interface Build {
	version: string;
	commit: string;
}

// isDevVersion treats a build as a "dev" build unless its version is exactly a
// semver release tag (optionally v-prefixed: "v1.2.3" / "1.2.3"). The match is
// anchored so a `git describe` fallback like "v1.2.3-15-gabc123" or
// "v1.2.3-dirty" is correctly classed as dev and shows its commit, rather than
// masquerading as the v1.2.3 release.
export function isDevVersion(version: string): boolean {
	return !/^v?\d+\.\d+\.\d+$/.test(version);
}

// stampedCommit reports whether a commit string names an actual build. Mirrors
// the backend's sentinel: a binary built without the commit ldflag reports
// "unknown", and a member too old to carry app_commit reports nothing.
export function stampedCommit(commit: string): boolean {
	return commit !== "" && commit !== "unknown";
}

// buildLabel is what to show for a build. A release tag identifies itself, so it
// wins. A dev version does not: every untagged image reports the same "dev"
// placeholder, so a column of them says nothing about whether the fleet agrees,
// and the commit is the only part that does.
export function buildLabel(build: Build): string {
	if (isDevVersion(build.version) && stampedCommit(build.commit)) {
		return build.commit;
	}
	return build.version;
}

// buildTitle is the hover detail behind the label: both halves, so a column
// showing commits still says which version they are, and vice versa. Undefined
// when there is no second half to add.
export function buildTitle(build: Build): string | undefined {
	if (build.version === "" || !stampedCommit(build.commit)) return undefined;
	return `${build.version} · ${build.commit}`;
}

// buildsDiffer mirrors buildSkew in internal/frontdesk/versionskew.go, which is
// what actually holds config sync. Keeping the badge's verdict and the gate's
// verdict derived from the same rule is the point: a member held by the backend
// while the UI shows it aligned is worse than no badge at all.
//
// The version decides it where the versions differ. Where they match - as they
// always do on a fleet of "dev" images - the commit does, and only when both
// sides report a real one: an unanswerable commit falls back to the version
// verdict rather than manufacturing a difference.
export function buildsDiffer(a: Build, b: Build): boolean {
	if (a.version !== b.version) return true;
	if (!stampedCommit(a.commit) || !stampedCommit(b.commit)) return false;
	return a.commit !== b.commit;
}
