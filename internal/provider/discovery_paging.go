package provider

import "fmt"

// maxDiscoveryPages bounds one cursor-paged model listing. The pagers ask for
// 100 rows a page, so this is 10,000 models, well past any real catalogue.
const maxDiscoveryPages = 100

// pagingError is a fault in the listing's own paging, as opposed to a failed
// fetch of one page: the upstream answered, and its answer cannot be followed.
type pagingError string

func (e pagingError) Error() string { return string(e) }

var (
	errPageCursorMissing  = pagingError("upstream reported more pages without a cursor")
	errPageCursorRepeated = pagingError("upstream repeated a pagination cursor")
	errPageLimit          = pagingError(fmt.Sprintf("model listing exceeded %d pages", maxDiscoveryPages))
)

// pageCursors records the cursors one paged model listing has followed. The
// cursor is the upstream's to choose, so a listing that hands back one it
// already gave, or none at all, would refetch the same page until the scan's
// deadline. The walk fails instead: a scan that returned the pages read so far
// would count the models on the unread pages as missing, and repeated scans
// disable a missing model.
type pageCursors map[string]struct{}

// advance accepts next as the cursor of the following page, or says why the
// walk has to stop.
func (c pageCursors) advance(next string) error {
	if next == "" {
		return errPageCursorMissing
	}
	if _, seen := c[next]; seen {
		return errPageCursorRepeated
	}
	// One cursor is followed per page after the first.
	if len(c) >= maxDiscoveryPages-1 {
		return errPageLimit
	}
	c[next] = struct{}{}
	return nil
}

// cursorForLog renders an upstream cursor for a debuglog line: quoted, so
// control characters cannot forge a line, and cut to 64 characters.
func cursorForLog(cursor string) string {
	return fmt.Sprintf("%.64q", cursor)
}
