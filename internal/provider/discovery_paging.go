package provider

import (
	"errors"
	"fmt"
)

// maxDiscoveryPages bounds one cursor-paged model listing. The pagers ask for
// 100 rows a page, so this is 10,000 models, well past any real catalogue.
const maxDiscoveryPages = 100

var (
	errPageCursorMissing  = errors.New("upstream reported more pages without a cursor")
	errPageCursorRepeated = errors.New("upstream repeated a pagination cursor")
	errPageLimit          = fmt.Errorf("model listing exceeded %d pages", maxDiscoveryPages)
)

// pageCursors records the cursors one paged model listing has followed. The
// cursor is the upstream's to choose, so a listing that hands back one it
// already gave, or none at all, would refetch the same page until the scan's
// deadline. The walk fails instead: a scan that returned the pages read so far
// would disable every model on the pages it never reached.
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
