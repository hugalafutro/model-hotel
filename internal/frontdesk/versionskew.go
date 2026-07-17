package frontdesk

// versionSkew reports whether a member's app version differs from the
// primary's. Comparison is exact-string on app_version (never the commit
// SHA), so "dev" == "dev" passes even across different commits; that gap is
// surfaced to the operator by the wizard's dev-fleet acknowledgment instead.
// An empty version (never polled, or the last fetch failed) fails closed:
// we never overwrite a member whose version we cannot confirm.
func versionSkew(primaryVer, memberVer string) bool {
	if primaryVer == "" || memberVer == "" {
		return true
	}
	return primaryVer != memberVer
}
