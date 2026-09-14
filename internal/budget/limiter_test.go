package budget

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
}

func (f *fakeSource) Spend(_ context.Context, kind, id string, _ time.Time) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return 0, f.err
	}
	return f.spent[kind+":"+id], nil
}

func newTestLimiter(src *fakeSource, at time.Time) (*Limiter, *time.Time) {
	l := NewLimiter(src)
	now := at
	l.now = func() time.Time { return now }
	return l, &now
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
	src := &fakeSource{spent: map[string]float64{"key:k1": 10}}
	at := time.Date(2026, 9, 16, 23, 0, 0, 0, time.UTC)
	l, _ := newTestLimiter(src, at)
	drain := collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}

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
	evs := drain()
	if len(evs) != 1 || evs[0].Type != "budget.exceeded" || evs[0].Severity != "error" {
		t.Fatalf("events = %+v, want one budget.exceeded", evs)
	}
	if !strings.Contains(evs[0].Message, `virtual key "ci" has spent $10.00 of its $10.00 day budget`) {
		t.Errorf("message = %q", evs[0].Message)
	}
}

func TestMiddleware_WarnsOnceAtEightyPercentThenAdmits(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"user:u1": 8}}
	l, _ := newTestLimiter(src, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	drain := collectEvents(t)
	sub := &Subject{Kind: KindUser, ID: "u1", Name: "alice", Budget: Budget{USD: 10, Period: PeriodMonth}}

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

func TestMiddleware_BothSubjectsMustPass(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 1, "user:u1": 50}}
	l, _ := newTestLimiter(src, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	collectEvents(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.KeyBudgetKey, &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}})
	ctx = context.WithValue(ctx, ctxkeys.UserBudgetKey, &Subject{Kind: KindUser, ID: "u1", Name: "alice", Budget: Budget{USD: 50, Period: PeriodMonth}})
	rr := serve(l, r.WithContext(ctx))
	if rr.Code != http.StatusTooManyRequests || !strings.Contains(rr.Body.String(), "user budget exceeded") {
		t.Fatalf("status = %d body = %s, want the user's refusal", rr.Code, rr.Body.String())
	}
	// No subject on the context: the stage is a pass-through.
	if rr := serve(l, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)); rr.Code != http.StatusNoContent {
		t.Fatalf("unbudgeted request status = %d", rr.Code)
	}
}

func TestLimiter_ChargeCountsBetweenReloadsAndPeriodRollResets(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 9}}
	at := time.Date(2026, 9, 16, 23, 59, 0, 0, time.UTC)
	l, now := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}

	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("first request status = %d", rr.Code)
	}
	// A charge inside the refresh window counts without another read.
	l.Charge("k1", "", 1.5)
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("after the charge status = %d, want 429", rr.Code)
	}
	if src.reads != 1 {
		t.Fatalf("source reads = %d, want 1 (the charge must not trigger a reload)", src.reads)
	}
	// A charge for a subject never admitted is dropped, not stored.
	l.Charge("k-unknown", "u-unknown", 100)
	if _, ok := l.entries.Load("key:k-unknown"); ok {
		t.Fatal("a charge must not create an entry")
	}
	// Midnight: a new period starts from the source's figure for it.
	src.mu.Lock()
	src.spent["key:k1"] = 0
	src.mu.Unlock()
	*now = at.Add(2 * time.Minute)
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("new period status = %d, want admitted", rr.Code)
	}
	if src.reads != 2 {
		t.Fatalf("source reads = %d, want 2 (the roll reloads)", src.reads)
	}
	// Inside the window the cached figure stands; past it the source is asked again.
	*now = now.Add(refreshInterval - time.Second)
	serve(l, keyed(sub))
	*now = now.Add(2 * time.Second)
	serve(l, keyed(sub))
	if src.reads != 3 {
		t.Fatalf("source reads = %d, want 3", src.reads)
	}
}

func TestLimiter_SourceFailureAdmitsOnLastKnownFigure(t *testing.T) {
	src := &fakeSource{spent: map[string]float64{"key:k1": 4}}
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	l, now := newTestLimiter(src, at)
	collectEvents(t)
	sub := &Subject{Kind: KindKey, ID: "k1", Name: "ci", Budget: Budget{USD: 10, Period: PeriodDay}}
	serve(l, keyed(sub))

	src.mu.Lock()
	src.err = errors.New("store down")
	src.spent["key:k1"] = 99
	src.mu.Unlock()
	*now = at.Add(2 * refreshInterval)
	if rr := serve(l, keyed(sub)); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want admitted on the last known $4", rr.Code)
	}
	if got := l.Spent(context.Background(), sub); got != 4 {
		t.Fatalf("Spent = %v, want the last known 4", got)
	}
	// The failing store is not asked again inside the interval.
	serve(l, keyed(sub))
	if src.reads != 2 {
		t.Fatalf("source reads = %d, want 2", src.reads)
	}
}

func TestLimiter_NilIsInert(t *testing.T) {
	var l *Limiter
	l.Charge("k", "u", 1)
	rr := httptest.NewRecorder()
	l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).
		ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
}
