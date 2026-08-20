package com.hugalafutro.bellhop.data

// A member's build identity: the app version it reports plus the commit that
// version was built from. Both are stamped into the binary at image build time,
// so neither can drift from the running code.
//
// Mirrors buildLabel/buildTitle in frontdesk/web/src/utils/build.ts and the
// verdict half of buildSkew in internal/frontdesk/versionskew.go. The three
// surfaces must agree: a member Front Desk shows as one build and Bellhop shows
// as another is worse than either showing nothing.

/** The sentinel a binary built without the commit ldflag reports. */
private const val UNSTAMPED_COMMIT = "unknown"

private val RELEASE_TAG = Regex("""^v?\d+\.\d+\.\d+$""")

/**
 * isDevVersion treats a build as a "dev" build unless its version is exactly a
 * semver release tag. Anchored, so a `git describe` fallback like
 * "v1.2.3-15-gabc123" is a dev build rather than the tag it derives from.
 */
fun isDevVersion(version: String): Boolean = !RELEASE_TAG.matches(version)

/** stampedCommit reports whether a commit string names an actual build. */
fun stampedCommit(commit: String): Boolean = commit.isNotBlank() && commit != UNSTAMPED_COMMIT

/**
 * buildLabel is the short identity for a tight space (the dashboard card). A
 * release tag identifies itself, so it wins; a dev version does not, because
 * every untagged image reports the same "dev" placeholder and a fleet of them
 * reads identical, so the commit is the only part that says which build this is.
 */
fun buildLabel(
    version: String,
    commit: String,
): String = if (isDevVersion(version) && stampedCommit(commit)) commit else version

/**
 * buildDetail is the long form for the detail screen, where there is room for
 * both halves: "dev · b80c04d4494f". Falls back to whichever half exists.
 */
fun buildDetail(
    version: String,
    commit: String,
): String =
    when {
        version.isBlank() -> ""
        stampedCommit(commit) -> "$version · $commit"
        else -> version
    }
