package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// pool provably takes 3. The live path settles the drawer's own slot
	// before cutting (recordRateLimitOutcome), so cut sees the load that fit.
	l.release(id, false, 0, 0)
	l.cut(id, 0)

	if got := l.windowFor(t, id).limit; got != 3 {
		t.Errorf("limit after cut with 3 survivors in flight = %d, want 3", got)
	}
	if l.tryAcquire(id, 0) {
		t.Error("a 4th acquisition was admitted over a limit of 3 with 3 still in flight")
	}
	// Never below one: a provider that answered at all has at least a slot.
	l2 := newInflightLimiter()
	if !l2.tryAcquire(id, 0) {
		t.Fatal("first acquisition refused")
	}
	l2.release(id, false, 0, 0)
	l2.cut(id, 0)
	if got := l2.windowFor(t, id).limit; got != 1 {
		t.Errorf("limit after cut with nothing left in flight = %d, want the floor of 1", got)
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
	// A hint never widens an existing, tighter cap: settle one drawer and cut
	// to a limit of 1, then hint again with the other still in flight.
	l.release(id, false, 0, 0)
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
	// An attempt that held no slot (limiter disabled) settles nothing.
	var slot *attemptSlot
	slot.settle(true)
}

// Only a literal "0" in one of the OpenAI-style remaining headers is a spent
// budget; anything else, malformed or generous, is no signal.
func TestRemainingBudgetZero(t *testing.T) {
	for _, key := range []string{"X-RateLimit-Remaining-Requests", "X-RateLimit-Remaining-Tokens", "X-RateLimit-Remaining"} {
		hdr := http.Header{}
		hdr.Set(key, "0")
		if !remainingBudgetZero(hdr) {
			t.Errorf("%s: 0 not read as a spent budget", key)
		}
	}
	for _, val := range []string{"", "1", "00", "0.0", "-0", "zero"} {
		hdr := http.Header{}
		hdr.Set("X-RateLimit-Remaining-Requests", val)
		if remainingBudgetZero(hdr) {
			t.Errorf("remaining %q read as a spent budget", val)
		}
	}
}

// A remaining=0 header on an otherwise fine response caps the window at the
// load in flight without waiting for the 429 it foretells, and the body still
// carries the slot so it settles on the last byte.
func TestFinishAttemptAdmission_RemainingZeroCapsTheWindow(t *testing.T) {
	h := &Handler{inflight: newInflightLimiter()}
	cand := modelCandidateForBreaker(uuid.New())
	if !h.inflight.tryAcquire(cand.provider.ID, 0) {
		t.Fatal("setup: slot not acquired")
	}
	settled := false
	st := &requestState{attemptSlot: &attemptSlot{fire: func(bool) { settled = true }}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}
	resp.Header.Set("X-RateLimit-Remaining-Requests", "0")

	h.finishAttemptAdmission(st, cand, resp)

	if got := h.inflight.windowFor(t, cand.provider.ID).limit; got != 1 {
		t.Errorf("limit after remaining=0 with one in flight = %d, want 1", got)
	}
	if _, ok := resp.Body.(*inflightRelease); !ok {
		t.Fatalf("body is %T, want the slot-releasing wrapper", resp.Body)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !settled {
		t.Error("the slot did not settle on the body's last byte")
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
		// The pool serves poolSize; every admission past it is a 429. The
		// drawer's slot settles before the cut, exactly as the live path
		// orders it (recordRateLimitOutcome).
		for i := poolSize; i < admitted; i++ {
			l.release(id, false, growAfter, time.Hour)
			l.cut(id, 0)
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
	// Both providers carry a hard ceiling of 1, so the admission the test
	// exercises is the same one retryAfterSlotFrees derives via providerCeiling.
	one := 1
	mkCand := func() modelCandidate {
		c := modelCandidateForBreaker(uuid.New())
		c.provider.MaxInFlight = &one
		return c
	}
	cands := []modelCandidate{mkCand(), mkCand()}

	t.Run("a busy entry spills to the next in priority, paying no backoff", func(t *testing.T) {
		st := newState()
		var tried []int
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, attempt, _ int) candidateOutcome {
			tried = append(tried, attempt)
			if c.provider.ID == cands[0].provider.ID {
				return outcomeBusy
			}
			return outcomeServed
		}
		start := time.Now()
		h.runFailoverLoop(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cands, fn)
		if len(tried) != 2 {
			t.Fatalf("attempts = %v, want the busy skip and the serve", tried)
		}
		// A busy skip contacted nothing, so the walk must not serve it the
		// exponential failover backoff (~100ms+) meant for failing providers.
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Errorf("walk took %v: a busy skip is paying the failover backoff", elapsed)
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

	// holdAll takes both providers' only slots and returns them at cleanup.
	holdAll := func(t *testing.T) {
		t.Helper()
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
	}
	busyUnlessRoom := func(c modelCandidate) candidateOutcome {
		if !h.inflight.canAdmit(c.provider.ID, 1) {
			return outcomeBusy
		}
		return outcomeServed
	}

	t.Run("a client that leaves mid-wait gets a 499, not an all-busy error", func(t *testing.T) {
		st := newState()
		holdAll(t)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, _, _ int) candidateOutcome {
			return busyUnlessRoom(c)
		}
		w := httptest.NewRecorder()
		h.runFailoverLoop(w, httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).WithContext(ctx), st, cands, fn)
		if w.Code != statusClientClosedRequest {
			t.Errorf("status = %d, want 499 for a client that hung up while every provider was full; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("losing the acquisition race keeps waiting inside the same window", func(t *testing.T) {
		st := newState()
		holdAll(t)
		var afterFree []candidateOutcome
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, _, _ int) candidateOutcome {
			if !h.inflight.canAdmit(c.provider.ID, 1) {
				return outcomeBusy
			}
			// First retry after the slot freed: a concurrent request took it
			// (simulated by answering busy while the slot is still free), the
			// second finds it and serves.
			if len(afterFree) == 0 {
				afterFree = append(afterFree, outcomeBusy)
				return outcomeBusy
			}
			afterFree = append(afterFree, outcomeServed)
			return outcomeServed
		}
		go func() {
			time.Sleep(30 * time.Millisecond)
			h.inflight.release(cands[1].provider.ID, true, 0, 0)
		}()
		h.runFailoverLoop(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cands, fn)
		if len(afterFree) != 2 || afterFree[1] != outcomeServed {
			t.Errorf("outcomes after the slot freed = %v, want a lost race followed by a serve", afterFree)
		}
	})

	t.Run("a freed slot that answers a real failure ends the walk on that verdict", func(t *testing.T) {
		st := newState()
		holdAll(t)
		retried := false
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, _, _ int) candidateOutcome {
			if !h.inflight.canAdmit(c.provider.ID, 1) {
				return outcomeBusy
			}
			retried = true
			return outcomeFailover
		}
		go func() {
			time.Sleep(30 * time.Millisecond)
			h.inflight.release(cands[1].provider.ID, true, 0, 0)
		}()
		w := httptest.NewRecorder()
		h.runFailoverLoop(w, httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cands, fn)
		if !retried {
			t.Fatal("the freed slot was never tried")
		}
		if w.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want the exhaustion 502 for a freed slot that failed; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a wait that keeps losing the race ends at the deadline", func(t *testing.T) {
		st := newState()
		st.overallDeadline = time.Now().Add(60 * time.Millisecond)
		holdAll(t)
		lost := 0
		fn := func(_ http.ResponseWriter, _ *http.Request, _ *requestState, c modelCandidate, _, _ int) candidateOutcome {
			if h.inflight.canAdmit(c.provider.ID, 1) {
				lost++
			}
			return outcomeBusy
		}
		go func() {
			time.Sleep(10 * time.Millisecond)
			h.inflight.release(cands[1].provider.ID, true, 0, 0)
		}()
		start := time.Now()
		w := httptest.NewRecorder()
		h.runFailoverLoop(w, httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cands, fn)
		if lost == 0 {
			t.Fatal("the freed slot was never retried")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("walk took %v: a lost race must stop at the deadline, not spin", elapsed)
		}
		if w.Code == http.StatusOK {
			t.Error("a walk that never served answered 200")
		}
	})
}
