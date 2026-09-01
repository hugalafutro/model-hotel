package proxy

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
)

// The adaptive per-provider in-flight limiter: saturation happens because the
// gateway sends a fourth request to a provider that can take three, and almost
// no provider states its concurrency limit in a machine-readable way. So
// rather than know the number, learn it the way TCP does: cut the allowance on
// a saturated 429, grow it back on clean completions, and forget it entirely
// after a quiet stretch. A provider that never saturates stays uncapped and
// costs one counter increment per request.
//
// State is per provider (slots are an account property; the circuit breaker
// stays per model — the two answer different questions, full vs broken), per
// member, in memory: runtime health, not config, never synced. Four members
// each learn their own allowance against a shared pool and the sum converges
// because each member's cuts are driven by the 429s it draws itself.
//
// The operator's providers.max_in_flight is a hard ceiling on top; the learner
// still runs underneath it.

const (
	// defaultInflightGrowAfter is how many consecutive clean completions earn
	// the window +1. Runtime override: inflight_grow_after.
	defaultInflightGrowAfter = 20
	// defaultInflightForgetAfter is how long without a cut before a capped
	// window returns to uncapped. Runtime override: inflight_forget_after.
	defaultInflightForgetAfter = 10 * time.Minute
)

// inflightWindow is one provider's learned allowance on this member.
type inflightWindow struct {
	limit    int       // current allowance; 0 = uncapped (initial state)
	inflight int       // requests currently between send and last byte
	goodRuns int       // consecutive clean completions since the last cut
	lastCut  time.Time // when the allowance was last cut (may sit in the near future: a Retry-After defers the forget clock)
}

// inflightLimiter holds every provider's window. All methods are nil-safe:
// a nil limiter admits everything and learns nothing, which is what the
// handler-literal test fixtures get.
type inflightLimiter struct {
	mu      sync.Mutex
	windows map[uuid.UUID]*inflightWindow
	// notify is closed and replaced whenever a slot frees or a window widens,
	// so an all-busy walk can wait for the first release anywhere instead of
	// polling.
	notify chan struct{}
}

func newInflightLimiter() *inflightLimiter {
	return &inflightLimiter{windows: make(map[uuid.UUID]*inflightWindow), notify: make(chan struct{})}
}

// effectiveLimit folds the learned allowance and the operator ceiling; 0 means
// uncapped.
func effectiveLimit(learned, ceiling int) int {
	switch {
	case ceiling <= 0:
		return learned
	case learned <= 0 || ceiling < learned:
		return ceiling
	default:
		return learned
	}
}

// tryAcquire admits one request to the provider, or reports it busy. ceiling
// is the operator's max_in_flight (0 = none).
func (l *inflightLimiter) tryAcquire(providerID uuid.UUID, ceiling int) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.window(providerID)
	if limit := effectiveLimit(w.limit, ceiling); limit > 0 && w.inflight >= limit {
		return false
	}
	w.inflight++
	return true
}

// canAdmit is tryAcquire without the acquisition, for the all-busy wait's
// cheap re-check. The answer can be stale by the time the caller acts on it;
// the caller's tryAcquire is the authority.
func (l *inflightLimiter) canAdmit(providerID uuid.UUID, ceiling int) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.window(providerID)
	limit := effectiveLimit(w.limit, ceiling)
	return limit <= 0 || w.inflight < limit
}

// release returns one slot. clean says the request completed as a success
// (a 2xx that was consumed to its end), which is what grows a capped window:
// +1 per growAfter consecutive clean completions, and back to uncapped once
// the window has gone forgetAfter without a cut. Failures only reset the
// clean-run count — shrinking is the cut's job, and only a saturated 429
// proves the allowance too high.
func (l *inflightLimiter) release(providerID uuid.UUID, clean bool, growAfter int, forgetAfter time.Duration) {
	if l == nil {
		return
	}
	if growAfter <= 0 {
		growAfter = defaultInflightGrowAfter
	}
	if forgetAfter <= 0 {
		forgetAfter = defaultInflightForgetAfter
	}
	l.mu.Lock()
	w := l.window(providerID)
	if w.inflight > 0 {
		w.inflight--
	}
	if w.limit > 0 {
		switch {
		case time.Since(w.lastCut) >= forgetAfter:
			// A quiet stretch: the congestion this window remembers is gone,
			// and holding a stale cap is a self-inflicted saturation.
			w.limit = 0
			w.goodRuns = 0
		case clean:
			w.goodRuns++
			if w.goodRuns >= growAfter {
				w.limit++
				w.goodRuns = 0
			}
		default:
			w.goodRuns = 0
		}
	}
	// Wake any all-busy waiter: a slot freed, or the window just widened.
	close(l.notify)
	l.notify = make(chan struct{})
	l.mu.Unlock()
}

// cut shrinks the provider's allowance after a SATURATED 429: the pool is
// provably smaller than the load that included the refused request. The CALLER
// settles the drawing request's own slot before cutting (see
// recordRateLimitOutcome), so w.inflight here is exactly the load that fit -
// the spec's "inflight - 1" with the subtraction made deterministic instead of
// racing the body reader that may already have released the drawer. retryAfter,
// when the provider sent one, pushes lastCut into the future so the forget
// clock starts after the wait the provider asked for.
func (l *inflightLimiter) cut(providerID uuid.UUID, retryAfter time.Duration) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.window(providerID)
	newLimit := max(1, w.inflight)
	if w.limit == 0 || newLimit < w.limit {
		w.limit = newLimit
	}
	w.goodRuns = 0
	w.lastCut = time.Now()
	if retryAfter > 0 {
		w.lastCut = w.lastCut.Add(retryAfter)
	}
	debuglog.Info("proxy: in-flight allowance cut on saturated 429", "provider_id", providerID, "limit", w.limit, "inflight", w.inflight)
}

// hintFull acts on a provider saying `remaining: 0` in its rate-limit headers:
// the pool is exactly full, so cap at the current in-flight count without
// waiting for the 429 that would otherwise establish it. Absent headers change
// nothing, and a hint never widens an existing cap.
func (l *inflightLimiter) hintFull(providerID uuid.UUID) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.window(providerID)
	newLimit := max(1, w.inflight)
	if w.limit == 0 || newLimit < w.limit {
		w.limit = newLimit
		w.goodRuns = 0
		w.lastCut = time.Now()
		debuglog.Info("proxy: in-flight allowance capped by remaining=0 header", "provider_id", providerID, "limit", w.limit)
	}
}

// waitForSlot blocks until admit reports room somewhere, the deadline passes,
// or ctx ends. admit is re-run on every release notification; true means the
// caller should try its acquisition now (which can still lose the race, in
// which case it simply waits again).
func (l *inflightLimiter) waitForSlot(ctx context.Context, deadline time.Time, admit func() bool) bool {
	if l == nil {
		return true
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		// The channel is snapshotted BEFORE the admission check: a release
		// landing between the two closes THIS channel, so the select below
		// fires immediately instead of subscribing to the replacement and
		// sleeping through a wakeup that already happened.
		l.mu.Lock()
		notify := l.notify
		l.mu.Unlock()
		if admit() {
			return true
		}
		select {
		case <-notify:
		case <-timer.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// window returns the provider's window, creating it on first use. Must be
// called with l.mu held.
func (l *inflightLimiter) window(providerID uuid.UUID) *inflightWindow {
	w, ok := l.windows[providerID]
	if !ok {
		w = &inflightWindow{}
		l.windows[providerID] = w
	}
	return w
}

// snapshot feeds the scrape-time gauges: one row per provider the limiter has
// ever tracked on this member.
func (l *inflightLimiter) snapshot() []metrics.InflightState {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]metrics.InflightState, 0, len(l.windows))
	for id, w := range l.windows {
		out = append(out, metrics.InflightState{ProviderID: id.String(), Limit: w.limit, Inflight: w.inflight})
	}
	return out
}

// attemptSlot is one attempt's held admission, settled exactly once whatever
// the attempt's fate: the wrapper below fires it when the response body is
// consumed or closed, the failure exits fire it directly, and the saturated
// 429 handler fires it BEFORE the cut so the cut's arithmetic never races the
// body reader (see cut). The sync.Once is what makes all of those safe to
// overlap.
type attemptSlot struct {
	once sync.Once
	fire func(clean bool)
}

// settle releases the slot. clean says the attempt completed as a consumed
// success; only the first settle counts, so a later duplicate (a drain closing
// a body whose EOF already fired, a forced settle before a cut) is a no-op.
func (s *attemptSlot) settle(clean bool) {
	if s == nil {
		return
	}
	s.once.Do(func() { s.fire(clean) })
}

// inflightRelease wraps an upstream body so the attempt's slot settles when
// the response has actually been consumed - the window counts requests
// "between send and last byte", and releasing at header time would let the
// gateway hold more streams open than the learned allowance. On EOF or Close,
// whichever comes first: every attempt path closes the body on every exit
// (client disconnect and hedge loss included), so a leaked count - a slow
// self-inflicted saturation - would need a leaked body, which the bodyclose
// lint already forbids.
type inflightRelease struct {
	io.ReadCloser
	slot  *attemptSlot
	clean bool
}

func (b *inflightRelease) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.slot.settle(b.clean)
	}
	return n, err
}

func (b *inflightRelease) Close() error {
	err := b.ReadCloser.Close()
	b.slot.settle(b.clean)
	return err
}

// admitCandidate is the admission gate an attempt passes before anything is
// built or sent: true admits (a slot is now held on st.attemptSlot and every
// exit MUST settle it), false means the provider's window is full and the
// candidate should be skipped the way a breaker-open one is. Disabled (or
// nil-limiter) always admits and holds nothing.
func (h *Handler) admitCandidate(st *requestState, candidate modelCandidate) bool {
	st.attemptSlot = nil
	if !st.inflightEnabled {
		return true
	}
	if !h.inflight.tryAcquire(candidate.provider.ID, providerCeiling(candidate)) {
		// The skip is recorded like a saturated 429 without the round trip:
		// same class, same terminal handling, no breaker involvement.
		st.rateLimit = rateLimitVerdict{classified: true, class: rateLimitSaturated, retryAfter: defaultSaturatedRetryAfter}
		debuglog.Info("proxy: skipping candidate: provider at in-flight limit", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
		return false
	}
	pid := candidate.provider.ID
	limiter := h.inflight
	settings := h.settingsRepo
	st.attemptSlot = &attemptSlot{fire: func(clean bool) {
		// Settings are read at settle time (a stream can end minutes after
		// admission) on a background context: the values are cached, and a
		// client's cancelled context must not turn a real read into defaults.
		ctx := context.Background()
		limiter.release(pid, clean,
			settings.GetInt(ctx, "inflight_grow_after", defaultInflightGrowAfter),
			settings.GetDuration(ctx, "inflight_forget_after", defaultInflightForgetAfter))
	}}
	return true
}

// finishAttemptAdmission hands the held slot to the response body once the
// attempt's effective status is known. It must run AFTER any status remap
// (MiniMax's 200-envelope rewrite): the clean flag decides whether the
// completion grows the window, and a business error dressed as a 200 must not.
// It also reads the provider's remaining-budget headers as a cut hint.
func (h *Handler) finishAttemptAdmission(st *requestState, candidate modelCandidate, resp *http.Response) {
	if st.attemptSlot == nil {
		return
	}
	if remainingBudgetZero(resp.Header) {
		h.inflight.hintFull(candidate.provider.ID)
	}
	resp.Body = &inflightRelease{ReadCloser: resp.Body, slot: st.attemptSlot, clean: servedSuccessStatus(resp.StatusCode)}
}

// remainingBudgetZero reports whether the provider's OpenAI-style rate-limit
// headers say a budget is exactly spent. Only a literal "0" counts: any other
// value, malformed or generous, is no signal.
func remainingBudgetZero(hdr http.Header) bool {
	for _, key := range []string{"X-RateLimit-Remaining-Requests", "X-RateLimit-Remaining-Tokens", "X-RateLimit-Remaining"} {
		if hdr.Get(key) == "0" {
			return true
		}
	}
	return false
}

// providerCeiling is the operator's hard max_in_flight for a candidate's
// provider; 0 = none.
func providerCeiling(candidate modelCandidate) int {
	if candidate.provider.MaxInFlight == nil {
		return 0
	}
	return *candidate.provider.MaxInFlight
}
