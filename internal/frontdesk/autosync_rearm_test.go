package frontdesk

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRearmWatchStopJoinsTheWatcher: the watcher reads the store, so a pass must
// join it, not merely cancel it. Cancelling alone lets a watcher that is already
// inside a query keep reading after the pass returned, past everything that waits
// on the pass, and that read then races the store's Close and the removal of the
// directory holding its database.
//
// The watcher is parked inside the cancel it calls on the rearm broadcast, which
// stands in for it being mid-query. stop must not return while it sits there.
func TestRearmWatchStopJoinsTheWatcher(t *testing.T) {
	srv, store := newTestServer(t)
	gen, err := store.AutoSyncGen(t.Context())
	if err != nil {
		t.Fatalf("AutoSyncGen: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rearmCh := make(chan struct{})
	inCancel := make(chan struct{})
	release := make(chan struct{})
	// Releasing on the way out as well as on the happy path keeps a failing run from
	// leaving the watcher parked forever.
	releaseWatcher := sync.OnceFunc(func() { close(release) })
	defer releaseWatcher()
	// cancel is called twice in the ordinary flow (the watcher on the rearm, then
	// stop), so only the first call parks: the second must return so stop can get
	// as far as waiting on the watcher.
	var parked atomic.Bool
	stop := srv.startRearmWatch(ctx, rearmCh, gen, func() {
		if parked.CompareAndSwap(false, true) {
			close(inCancel)
			<-release
		}
		cancel()
	})

	close(rearmCh) // the rearm broadcast: wakes the watcher into cancel
	<-inCancel     // the watcher is running and has not returned

	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	// An unjoined watcher makes stop return immediately; a joined one cannot
	// return until the watcher is released below. The window only has to outlast
	// a goroutine handoff.
	select {
	case <-stopped:
		t.Fatal("stop returned while the rearm watcher was still running")
	case <-time.After(50 * time.Millisecond):
	}

	releaseWatcher()
	<-stopped
}
