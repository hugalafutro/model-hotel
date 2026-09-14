package budget

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// fakeSource answers with a fixed sum per subject and counts the reads.
type fakeSource struct {
	mu    sync.Mutex
	spent map[string]float64
	err   error
	reads int
	block chan struct{} // when set, a read waits on it after counting itself
}

func (f *fakeSource) Spend(_ context.Context, kind, id string, _ time.Time) (float64, error) {
	f.mu.Lock()
	f.reads++
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	return f.spent[kind+":"+id], nil
}

func (f *fakeSource) set(key string, v float64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spent[key] = v
	f.err = err
}

func (f *fakeSource) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// waitReads blocks until the source has been read n times (a background
// reload landing) or fails the test.
func (f *fakeSource) waitReads(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.readCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("source reads = %d, want %d", f.readCount(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// clock is a settable time source shared with the limiter under test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestLimiter(src *fakeSource, at time.Time) (*Limiter, *clock) {
	l := NewLimiter(src)
	c := &clock{t: at}
	l.now = c.now
	return l, c
}

func keyed(sub *Subject) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	key := ctxkeys.KeyBudgetKey
	if sub.Kind == KindUser {
		key = ctxkeys.UserBudgetKey
	}
	return r.WithContext(context.WithValue(r.Context(), key, sub))
}

func serve(l *Limiter, r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(rr, r)
	return rr
}

func collectEvents(t *testing.T) func() []events.Event {
	t.Helper()
	ch := events.Subscribe()
	t.Cleanup(func() { events.Unsubscribe(ch) })
	return func() []events.Event {
		var out []events.Event
		for {
			select {
			case ev := <-ch:
				out = append(out, ev)
			case <-time.After(50 * time.Millisecond):
				return out
			}
		}
	}
}

func TestMiddleware_RefusesAtBudgetWithRetryAfterToPeriodEnd(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 5}}
	at := time.Date(2026, 9, 16, 23, 0, 0, 0, time.UTC)
	l, _ := newTestLimiter(src, at)
	drain := collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}

	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("under budget status = %d", rr.Code)
	}
	l.Charge("k1", "", 5, time.Time{})
	rr := serve(l, keyed(sub))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600 (one hour to midnight UTC)", got)
	}
	if !strings.Contains(rr.Body.String(), "key budget exceeded") || !strings.Contains(rr.Body.String(), "$10.00 of $10.00") {
		t.Errorf("body = %s", rr.Body.String())
	}
	// A second refusal in the same period does not repeat the event.
	serve(l, keyed(sub))
	// One jump past both thresholds reports the refusal, not the warning too.
	evs := drain()
	if len(evs) != 1 || evs[0].Type != "budget.exceeded" || evs[0].Severity != "error" {
		t.Fatalf("events = %+v, want one budget.exceeded", evs)
	}
	if !strings.Contains(evs[0].Message, `virtual key "ci" has spent $10.00 of its $10.00 day budget`) {
		t.Errorf("message = %q", evs[0].Message)
	}
}

func TestMiddleware_FirstSightPastAThresholdRefusesWithoutReporting(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 12, "user:u1": 8.5}}
	l, _ := newTestLimiter(src, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	drain := collectEvents(t)
	key := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}
	user := &Subject{Kind: KindUser, ID: "u1", Name: "alice", Budget: Budget{USD: 10, Period: PeriodMonth}}

	if rr := serve(l, keyed(key)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("over budget on first sight status = %d, want 429", rr.Code)
	}
	if rr := serve(l, keyed(user)); rr.Code != http.StatusNoContent {
		t.Fatalf("warned on first sight status = %d, want admitted", rr.Code)
	}
	if evs := drain(); len(evs) != 0 {
		t.Fatalf("a restart inside the period must not re-report: %+v", evs)
	}
	// Crossing the NEXT threshold in this process is reported.
	l.Charge("", "u1", 2, time.Time{})
	serve(l, keyed(user))
	if evs := drain(); len(evs) != 1 || evs[0].Type != "budget.exceeded" {
		t.Fatalf("events = %+v, want one budget.exceeded", evs)
	}
}

func TestMiddleware_WarnsOnceAtEightyPercentThenAdmits(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"user:u1": 7}}
	l, _ := newTestLimiter(src, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	drain := collectEvents(t)
	sub := &Subject{Kind: KindUser, ID: "u1", Name: "alice", Budget: Budget{USD: 10, Period: PeriodMonth}}

	serve(l, keyed(sub))
	l.Charge("", "u1", 1, time.Time{})
	for range 2 {
		if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want admitted", rr.Code)
		}
	}
	evs := drain()
	if len(evs) != 1 || evs[0].Type != "budget.warning" || evs[0].Severity != "warning" {
		t.Fatalf("events = %+v, want one budget.warning", evs)
	}
	if !strings.Contains(evs[0].Message, `user "alice" has spent $8.00 of its $10.00 month budget`) {
		t.Errorf("message = %q", evs[0].Message)
	}
}

func TestMiddleware_BudgetEditedMidPeriodIsJudgedAfresh(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 9}}
	l, _ := newTestLimiter(src, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	drain := collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}
	serve(l, keyed(sub))
	l.Charge("k1", "", 1, time.Time{})
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}
	drain()
	raised := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 20, Period: PeriodDay}}
	if rr := serve(l, keyed(raised)); rr.Code != http.StatusNoContent {
		t.Fatalf("raised budget status = %d, want admitted", rr.Code)
	}
	if evs := drain(); len(evs) != 0 {
		t.Fatalf("raising the budget must not report: %+v", evs)
	}
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("lowered back status = %d, want 429", rr.Code)
	}
	if evs := drain(); len(evs) != 1 || evs[0].Type != "budget.exceeded" {
		t.Fatalf("events = %+v, want the exceeded event again for the lowered budget", evs)
	}
}

func TestMiddleware_BothSubjectsJudgedAndTheLaterPeriodSetsRetryAfter(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 10, "user:u1": 50}}
	at := time.Date(2026, 9, 16, 23, 0, 0, 0, time.UTC)
	l, _ := newTestLimiter(src, at)
	collectEvents(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.KeyBudgetKey, &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}})
	ctx = context.WithValue(ctx, ctxkeys.UserBudgetKey, &Subject{Kind: KindUser, ID: "u1", Name: "alice", Budget: Budget{USD: 50, Period: PeriodMonth}})
	rr := serve(l, r.WithContext(ctx))
	if rr.Code != http.StatusTooManyRequests || !strings.Contains(rr.Body.String(), "key budget exceeded") {
		t.Fatalf("status = %d body = %s, want the key's refusal named", rr.Code, rr.Body.String())
	}
	secs, _ := strconv.Atoi(rr.Header().Get("Retry-After"))
	if want := int(PeriodEnd(PeriodMonth, at).Sub(at).Seconds()); secs != want {
		t.Errorf("Retry-After = %d, want %d (the user's month, the later period)", secs, want)
	}
	// Only the user over: the user's refusal is named.
	src.set("key:k1", 1, nil)
	l2, _ := newTestLimiter(src, at)
	if rr := serve(l2, r.WithContext(ctx)); rr.Code != http.StatusTooManyRequests || !strings.Contains(rr.Body.String(), "user budget exceeded") {
		t.Fatalf("status = %d body = %s, want the user's refusal", rr.Code, rr.Body.String())
	}
	// No subject on the context: the stage is a pass-through. So is a read.
	if rr := serve(l, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)); rr.Code != http.StatusNoContent {
		t.Fatalf("unbudgeted request status = %d", rr.Code)
	}
	get := httptest.NewRequest(http.MethodGet, "/v1/models", http.NoBody).WithContext(ctx)
	if rr := serve(l, get); rr.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want admitted: a listing spends nothing", rr.Code)
	}
}

func TestLimiter_ChargeCountsBetweenReloadsAndPeriodRollResets(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 9}}
	at := time.Date(2026, 9, 16, 23, 59, 0, 0, time.UTC)
	l, clk := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}

	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("first request status = %d", rr.Code)
	}
	// A charge inside the refresh window counts without another read.
	l.Charge("k1", "", 1.5, time.Time{})
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("after the charge status = %d, want 429", rr.Code)
	}
	if src.readCount() != 1 {
		t.Fatalf("source reads = %d, want 1 (the charge must not trigger a reload)", src.readCount())
	}
	// A charge for a subject never admitted is dropped, not stored.
	l.Charge("k-unknown", "u-unknown", 100, time.Time{})
	if _, ok := l.entries.Load("key:k-unknown"); ok {
		t.Fatal("a charge must not create an entry")
	}
	// Midnight: a new period starts from the source's figure for it, summed
	// before the answer.
	src.set("key:k1", 0, nil)
	clk.add(2 * time.Minute)
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("new period status = %d, want admitted", rr.Code)
	}
	if src.readCount() != 2 {
		t.Fatalf("source reads = %d, want 2 (the roll reloads)", src.readCount())
	}
}

func TestLimiter_StaleFigureServedWhileOneReloadReplacesIt(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 4}}
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	l, clk := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}
	serve(l, keyed(sub))

	// Inside the window the cached figure stands.
	clk.add(refreshInterval - time.Second)
	src.set("key:k1", 40, nil)
	serve(l, keyed(sub))
	if src.readCount() != 1 {
		t.Fatalf("source reads = %d, want 1", src.readCount())
	}
	// Past it the request is answered from the stale figure at once and one
	// reload brings the store's figure in behind it.
	clk.add(2 * time.Second)
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("stale-window status = %d, want admitted on the old $4", rr.Code)
	}
	src.waitReads(t, 2)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := l.Spent(context.Background(), sub)
		if got == 40 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Spent = %v, want the reloaded 40", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("after the reload status = %d, want 429", rr.Code)
	}
}

func TestLimiter_SourceFailureAdmitsOnLastKnownFigureButRefusesTheUnknown(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 4}}
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	l, clk := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}
	serve(l, keyed(sub))

	src.set("key:k1", 99, errors.New("store down"))
	clk.add(2 * refreshInterval)
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want admitted on the last known $4", rr.Code)
	}
	src.waitReads(t, 2)
	if got, known := l.Spent(context.Background(), sub); !known || got != 4 {
		t.Fatalf("Spent = %v/%v, want the last known 4", got, known)
	}
	// A subject first seen while the store is down cannot be judged: it is
	// refused with 503, and the store is asked again only after the retry
	// interval, not on every request.
	fresh := &Subject{Kind: KindKey, ID: "k2", Name: "new", Budget: Budget{USD: 1, Period: PeriodDay}}
	rr := serve(l, keyed(fresh))
	if rr.Code != http.StatusServiceUnavailable || rr.Header().Get("Retry-After") != "5" {
		t.Fatalf("first-sight failure status = %d Retry-After %q, want 503 in 5s", rr.Code, rr.Header().Get("Retry-After"))
	}
	if !strings.Contains(rr.Body.String(), "key budget spend unavailable") {
		t.Errorf("body = %s", rr.Body.String())
	}
	reads := src.readCount()
	serve(l, keyed(fresh))
	if src.readCount() != reads {
		t.Fatalf("source reads = %d, want %d (no retry inside the interval)", src.readCount(), reads)
	}
	if _, known := l.Spent(context.Background(), fresh); known {
		t.Fatal("Spent must report an unknown figure while the store is down")
	}
	// Once the store answers, the retry judges the subject.
	src.set("key:k2", 0.5, nil)
	clk.add(retryInterval)
	if rr := serve(l, keyed(fresh)); rr.Code != http.StatusNoContent {
		t.Fatalf("after the store recovered status = %d, want admitted", rr.Code)
	}
}

func TestLimiter_ReloadRunsAgainWhenAChargeOverlappedIt(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 4}}
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	l, clk := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 100, Period: PeriodDay}}
	serve(l, keyed(sub))

	// Hold the source's answer until a charge has landed during the read:
	// the reload must not install a sum that may or may not hold that row.
	src.mu.Lock()
	src.block = make(chan struct{})
	src.mu.Unlock()
	clk.add(refreshInterval + time.Second)
	serve(l, keyed(sub)) // schedules the reload, which now blocks in Spend
	src.waitReads(t, 2)
	l.Charge("k1", "", 3, time.Time{})
	src.set("key:k1", 7, nil) // the store now holds the charged row
	src.mu.Lock()
	close(src.block)
	src.block = nil
	src.mu.Unlock()
	// The overlapped reload re-runs and lands the exact 7, not 7+3 and not 4.
	src.waitReads(t, 3)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := l.Spent(context.Background(), sub)
		if got == 7 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Spent = %v, want the exact re-summed 7", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLimiter_ChargeFromThePreviousPeriodIsNotCountedHere(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 1}}
	at := time.Date(2026, 9, 17, 0, 0, 30, 0, time.UTC)
	l, _ := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}
	serve(l, keyed(sub))
	// A request that arrived before midnight and finished after it belongs
	// to yesterday's sum; one with no arrival time counts as now.
	l.Charge("k1", "", 5, at.Add(-time.Minute))
	l.Charge("k1", "", 2, time.Time{})
	if got, _ := l.Spent(context.Background(), sub); got != 3 {
		t.Fatalf("Spent = %v, want 1 + 2", got)
	}
}

func TestLimiter_NilIsInert(t *testing.T) {
	var l *Limiter
	l.Charge("k", "u", 1, time.Time{})
	rr := httptest.NewRecorder()
	l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).
		ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
}
