package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The learner's contract: cut on proof (a saturated 429 with the load that
// drew it still counted), grow on clean completions, forget after quiet, and
// cost nothing while a provider stays uncapped.

func (l *inflightLimiter) windowFor(t *testing.T, id uuid.UUID) *inflightWindow {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[id]
	if !ok {
		t.Fatalf("no window tracked for %s", id)
	}
	return w
}

func TestInflightLimiter_CutTargetsTheLoadThatFit(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()

	for range 4 {
		if !l.tryAcquire(id, 0) {
			t.Fatal("uncapped window refused an acquisition")
		}
	}
	// The 4th request drew a saturated 429 while all four were in flight: the
	// pool provably takes 3.
	l.cut(id, 0)

	if got := l.windowFor(t, id).limit; got != 3 {
		t.Errorf("limit after cut at 4 in flight = %d, want 3 (inflight-1)", got)
	}
	if l.tryAcquire(id, 0) {
		t.Error("a 5th acquisition was admitted over a limit of 3 with 4 still in flight")
	}
	// Never below one: a provider that answered at all has at least a slot.
	l2 := newInflightLimiter()
	if !l2.tryAcquire(id, 0) {
		t.Fatal("first acquisition refused")
	}
	l2.cut(id, 0)
	if got := l2.windowFor(t, id).limit; got != 1 {
		t.Errorf("limit after cut at 1 in flight = %d, want the floor of 1", got)
	}
}

func TestInflightLimiter_GrowsOnCleanRunsOnly(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	l.tryAcquire(id, 0)
	l.cut(id, 0) // limit 1

	const growAfter = 3
	// Two clean completions, then a failure: the run resets and the window
	// stays put.
	for range 2 {
		l.tryAcquire(id, 0)
		l.release(id, true, growAfter, time.Hour)
	}
	l.tryAcquire(id, 0)
	l.release(id, false, growAfter, time.Hour)
	if got := l.windowFor(t, id).limit; got != 1 {
		t.Fatalf("limit after a broken run = %d, want 1", got)
	}
	// Three clean in a row earn +1.
	for range growAfter {
		l.tryAcquire(id, 0)
		l.release(id, true, growAfter, time.Hour)
	}
	if got := l.windowFor(t, id).limit; got != 2 {
		t.Errorf("limit after %d clean completions = %d, want 2", growAfter, got)
	}
}

func TestInflightLimiter_ForgetsAfterQuiet(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	l.tryAcquire(id, 0)
	l.cut(id, 0)
	// Age the cut past the forget horizon instead of waiting it out.
	l.windowFor(t, id).lastCut = time.Now().Add(-11 * time.Minute)

	l.tryAcquire(id, 0)
	l.release(id, true, defaultInflightGrowAfter, 10*time.Minute)

	if got := l.windowFor(t, id).limit; got != 0 {
		t.Errorf("limit after a quiet stretch = %d, want 0 (uncapped): a stale cap is a self-inflicted saturation", got)
	}
}

func TestInflightLimiter_RetryAfterDefersTheForgetClock(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	l.tryAcquire(id, 0)
	// The provider asked for a 30s wait: the quiet clock starts after it.
	l.cut(id, 30*time.Second)

	if got := l.windowFor(t, id).lastCut; !got.After(time.Now().Add(20 * time.Second)) {
		t.Errorf("lastCut = %v, want pushed ~30s into the future by the provider's Retry-After", got)
	}
}

func TestInflightLimiter_HintFullCapsWithoutA429(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	l.tryAcquire(id, 0)
	l.tryAcquire(id, 0)

	l.hintFull(id) // remaining: 0 with 2 in flight

	if got := l.windowFor(t, id).limit; got != 2 {
		t.Fatalf("limit after remaining=0 at 2 in flight = %d, want 2", got)
	}
	// A hint never widens an existing, tighter cap.
	l.cut(id, 0) // limit 1
	l.hintFull(id)
	if got := l.windowFor(t, id).limit; got != 1 {
		t.Errorf("limit after hint over a cap of 1 = %d, want 1: hints only tighten", got)
	}
}

func TestInflightLimiter_OperatorCeilingBoundsTheUncappedWindow(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()

	for i := range 2 {
		if !l.tryAcquire(id, 2) {
			t.Fatalf("acquisition %d under the ceiling refused", i+1)
		}
	}
	if l.tryAcquire(id, 2) {
		t.Error("a third acquisition passed a max_in_flight of 2")
	}
	// The ceiling also bounds a wider learned window.
	if got := effectiveLimit(5, 2); got != 2 {
		t.Errorf("effectiveLimit(5, 2) = %d, want the ceiling", got)
	}
	if got := effectiveLimit(2, 5); got != 2 {
		t.Errorf("effectiveLimit(2, 5) = %d, want the learned limit", got)
	}
	if got := effectiveLimit(0, 0); got != 0 {
		t.Errorf("effectiveLimit(0, 0) = %d, want uncapped", got)
	}
}

// A provider that never saturates must cost nothing: the uncapped
// acquire/release pair allocates nothing once its window exists.
func TestInflightLimiter_UncappedPathAllocatesNothing(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	l.tryAcquire(id, 0)
	l.release(id, true, 0, 0)

	allocs := testing.AllocsPerRun(100, func() {
		l.tryAcquire(id, 0)
		l.release(id, true, 0, 0)
	})
	// One allocation per pair is the notify channel replaced on release; the
	// window itself must not allocate.
	if allocs > 1 {
		t.Errorf("uncapped acquire/release allocates %.1f objects per pair, want at most the release notification", allocs)
	}
}

func TestInflightLimiter_WaitForSlot(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	l.tryAcquire(id, 1) // the only slot

	admit := func() bool { return l.canAdmit(id, 1) }

	// A release from another goroutine wakes the waiter.
	go func() {
		time.Sleep(20 * time.Millisecond)
		l.release(id, true, 0, 0)
	}()
	if !l.waitForSlot(context.Background(), time.Now().Add(2*time.Second), admit) {
		t.Fatal("waitForSlot missed the released slot")
	}

	// A deadline with no release times out.
	l.tryAcquire(id, 1)
	if l.waitForSlot(context.Background(), time.Now().Add(30*time.Millisecond), admit) {
		t.Error("waitForSlot reported a slot that never freed")
	}

	// A cancelled context stops the wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if l.waitForSlot(ctx, time.Now().Add(2*time.Second), admit) {
		t.Error("waitForSlot ignored the cancelled context")
	}
}

// Nil is the handler-literal test fixture: everything admits, nothing panics.
func TestInflightLimiter_NilIsInert(t *testing.T) {
	var l *inflightLimiter
	id := uuid.New()
	if !l.tryAcquire(id, 1) || !l.canAdmit(id, 1) {
		t.Error("a nil limiter refused an acquisition")
	}
	l.release(id, true, 0, 0)
	l.cut(id, 0)
	l.hintFull(id)
	if got := l.snapshot(); got != nil {
		t.Errorf("nil snapshot = %v, want nil", got)
	}
	if !l.waitForSlot(context.Background(), time.Now(), func() bool { return false }) {
		t.Error("a nil limiter made a caller wait")
	}
}

// Four concurrent clients against a 3-slot pool: the learned allowance lands
// within one of the truth. Deterministic rounds rather than racing goroutines,
// so the test cannot flake on scheduling: each round the four clients arrive
// together, the pool serves three, and any overflow answers a saturated 429.
func TestInflightLimiter_ConvergesOnAThreeSlotPool(t *testing.T) {
	l := newInflightLimiter()
	id := uuid.New()
	const poolSize = 3
	const growAfter = 5

	for range 200 {
		admitted := 0
		for range 4 {
			if l.tryAcquire(id, 0) {
				admitted++
			}
		}
		// The pool serves poolSize; every admission past it is a 429. The cut
		// happens while the overflowing load is still in flight, exactly as
		// the live path does it.
		for i := poolSize; i < admitted; i++ {
			l.cut(id, 0)
			l.release(id, false, growAfter, time.Hour)
		}
		for i := 0; i < min(admitted, poolSize); i++ {
			l.release(id, true, growAfter, time.Hour)
		}
	}

	// The window oscillates between the pool size (grown past it, cut back)
	// and one above; anything further off means the learner diverged.
	if got := l.windowFor(t, id).limit; got < poolSize-1 || got > poolSize+1 {
		t.Errorf("learned limit = %d, want within one of the real pool size %d", got, poolSize)
	}
}

// The loop-level contract of section 10: a busy candidate is skipped without a
// request, the next entry takes it, and an all-busy walk waits for the first
// slot to free anywhere before answering anyone with an error.
func TestRunFailoverLoop_BusyCandidates(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler

	newState := func() *requestState {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "m", false)
		return &requestState{
			startTime:       time.Now(),
			reqModel:        "m",
			isFailover:      true,
			overallDeadline: time.Now().Add(time.Minute),
			inflightEnabled: true,
			logData:         logData,
		}
	}
	cands := []modelCandidate{modelCandidateForBreaker(uuid.New()), modelCandidateForBreaker(uuid.New())}

	t.Run("a busy entry spills to the next in priority", func(t *testing.T) {
		st := newState()
		var tried []int
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, attempt, _ int) candidateOutcome {
			tried = append(tried, attempt)
			if c.provider.ID == cands[0].provider.ID {
				return outcomeBusy
			}
			return outcomeServed
		}
		h.runFailoverLoop(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cands, fn)
		if len(tried) != 2 {
			t.Fatalf("attempts = %v, want the busy skip and the serve", tried)
		}
	})

	t.Run("all busy waits for the first freed slot and serves there", func(t *testing.T) {
		st := newState()
		// Both providers at a window of 1, both slots held.
		for _, c := range cands {
			if !h.inflight.tryAcquire(c.provider.ID, 1) {
				t.Fatal("setup: slot not acquired")
			}
		}
		t.Cleanup(func() {
			for _, c := range cands {
				h.inflight.release(c.provider.ID, true, 0, 0)
			}
		})
		served := false
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, _, _ int) candidateOutcome {
			if !h.inflight.canAdmit(c.provider.ID, 1) {
				return outcomeBusy
			}
			served = true
			return outcomeServed
		}
		// Candidate 1's slot frees shortly after the walk goes to sleep. (The
		// cleanup's extra release is harmless: release guards inflight > 0.)
		go func() {
			time.Sleep(30 * time.Millisecond)
			h.inflight.release(cands[1].provider.ID, true, 0, 0)
		}()
		h.runFailoverLoop(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cands, fn)
		if !served {
			t.Error("the freed slot was never used: the all-busy walk answered an error while capacity existed")
		}
	})
}
