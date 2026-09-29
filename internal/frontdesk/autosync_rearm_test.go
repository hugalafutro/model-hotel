package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
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

// gatedPrimary fronts a primary stub with a config-version read that blocks
// until release is closed, counting the reads and how many were in flight at
// once. Every other request goes straight to the stub.
type gatedPrimary struct {
	srv                          *httptest.Server
	release                      chan struct{}
	mu                           sync.Mutex
	reads, inFlight, maxInFlight int
}

func newGatedPrimary(t *testing.T, stub *stubAutoMember) *gatedPrimary {
	t.Helper()
	g := &gatedPrimary{release: make(chan struct{})}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config/version" {
			g.mu.Lock()
			g.reads++
			g.inFlight++
			g.maxInFlight = max(g.maxInFlight, g.inFlight)
			g.mu.Unlock()
			<-g.release
			defer func() { g.mu.Lock(); g.inFlight--; g.mu.Unlock() }()
		}
		stub.srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *gatedPrimary) counts() (reads, maxInFlight int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads, g.maxInFlight
}

// waitUntil polls cond until it holds or five seconds pass, and reports which.
func waitUntil(cond func() bool) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return false
}

// TestKickAutoSyncBurstCoalesces: a burst of enable-time kicks must not run a
// pass per kick. The first kick's pass is held at the primary's version read
// until every other kick has returned, so none can arrive late; coalesced, they
// each only leave a follow-up behind, and the burst costs two passes (the held
// one and one follow-up) and pushes the export once.
func TestKickAutoSyncBurstCoalesces(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B" // converged by the first push, so a later pass has nothing to send
	gated := newGatedPrimary(t, primary)

	pm, _ := store.CreateMember(t.Context(), "primary", gated.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	var wg sync.WaitGroup
	wg.Go(func() { srv.kickAutoSync(t.Context()) })
	if !waitUntil(func() bool { r, _ := gated.counts(); return r == 1 }) {
		t.Fatal("the first kick's pass never reached the primary")
	}
	const more = 4
	var returned atomic.Int32
	for range more {
		wg.Go(func() { srv.kickAutoSync(t.Context()); returned.Add(1) })
	}
	coalesced := waitUntil(func() bool { return returned.Load() == more })
	close(gated.release)
	wg.Wait()
	if !coalesced {
		t.Error("kicks arriving while a pass ran did not return at once")
	}

	reads, maxInFlight := gated.counts()
	if maxInFlight != 1 {
		t.Errorf("%d passes in flight at once, want 1", maxInFlight)
	}
	if reads != 2 {
		t.Errorf("%d passes for a burst of %d kicks, want 2 (the running one and one follow-up)", reads, more+1)
	}
	if got := primary.exportCount(); got != 1 {
		t.Errorf("export read %d times for one burst, want 1", got)
	}
	if got := replica.realSyncCount(); got != 1 {
		t.Errorf("replica took %d imports for one burst, want 1", got)
	}
}

// TestWaitingPassRereadsThePrimary: a pass queued behind another reads the
// primary's hash once it holds the pass lock, not before. The primary moves on
// while the pass waits, and the replica already holds the newer config, so a
// pass converging against the hash it would have read on arrival re-pushes a
// member that needs nothing.
func TestWaitingPassRereadsThePrimary(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.dryDiff = driftDiff })

	f.srv.passMu.Lock()
	done := make(chan struct{})
	go func() { f.srv.forceAutoSyncNow(t.Context()); close(done) }()
	// Blocked on the pass lock: whatever it reads, it reads after the move below.
	waitBlockedInLockPass(t)
	f.primary.setVersionHash("hash-C")
	f.replica.mu.Lock()
	f.replica.versionHash = "hash-C"
	f.replica.mu.Unlock()
	f.srv.passMu.Unlock()
	<-done

	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("replica holding the primary's current config was pushed %d times, want 0", got)
	}
	if !f.verified() {
		t.Error("the waiting pass did not verify the replica against the primary's current hash")
	}
}

// TestWaitingPassWithEndedContextRunsNothing: a pass queued behind another whose
// context ends while it waits (shutdown) runs nothing once it gets the lock.
func TestWaitingPassWithEndedContextRunsNothing(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.dryDiff = driftDiff })

	f.srv.passMu.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { f.srv.forceAutoSyncNow(ctx); close(done) }()
	waitBlockedInLockPass(t)
	cancel()
	f.srv.passMu.Unlock()
	<-done

	if got := f.primary.versionReadCount(); got != 0 {
		t.Errorf("a pass whose context ended while it waited read the primary %d times, want 0", got)
	}
	if got := f.replica.versionReadCount(); got != 0 {
		t.Errorf("a pass whose context ended while it waited measured the replica %d times, want 0", got)
	}
}

// waitBlockedInLockPass returns once a goroutine is parked on the pass lock
// inside lockPass, read from the goroutine dump: the pass has started and
// reads nothing until the test releases passMu.
func waitBlockedInLockPass(t *testing.T) {
	t.Helper()
	buf := make([]byte, 1<<20)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		for g := range strings.SplitSeq(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
			if strings.Contains(g, "[sync.Mutex.Lock") && strings.Contains(g, "(*Server).lockPass") {
				return
			}
		}
	}
	t.Fatal("no pass blocked on the pass lock")
}
