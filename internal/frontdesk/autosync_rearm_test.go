package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// TestPutAutoSyncBurstCoalescesKicks: a burst of PUT /api/fleet/autosync must
// not run a pass per PUT. The primary's version read is held until the burst is
// over, which is where every kick's pass would sit side by side; coalesced, only
// one pass is ever in flight there, and the burst pushes the export once.
func TestPutAutoSyncBurstCoalescesKicks(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B" // converged by the first push, so a later pass has nothing to send

	var mu sync.Mutex
	var inFlight, maxInFlight int
	gate := make(chan struct{})
	gated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config/version" {
			mu.Lock()
			inFlight++
			maxInFlight = max(maxInFlight, inFlight)
			mu.Unlock()
			<-gate
			defer func() { mu.Lock(); inFlight--; mu.Unlock() }()
		}
		primary.srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(gated.Close)

	pm, _ := store.CreateMember(t.Context(), "primary", gated.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	const puts = 5
	var wg sync.WaitGroup
	for range puts {
		wg.Go(func() {
			if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":"`+pm.ID+`"}`, true); rec.Code != http.StatusOK {
				t.Errorf("put autosync = %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
	wg.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for maxInFlightNow := 0; maxInFlightNow == 0 && time.Now().Before(deadline); {
		mu.Lock()
		maxInFlightNow = maxInFlight
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // any uncoalesced kick reaches the gate by now
	close(gate)
	srv.Wait()

	mu.Lock()
	got := maxInFlight
	mu.Unlock()
	if got != 1 {
		t.Errorf("%d passes in flight at once, want 1", got)
	}
	if got := primary.exportCount(); got != 1 {
		t.Errorf("export read %d times for one burst, want 1", got)
	}
	if got := replica.realSyncCount(); got != 1 {
		t.Errorf("replica took %d imports for one burst, want 1", got)
	}
}
