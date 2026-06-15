package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// newTestTPMLimiter builds a TPMLimiter with a stub settings backend and stops
// its cleanup goroutine via t.Cleanup.
func newTestTPMLimiter(t *testing.T) (*TPMLimiter, *stubSettings) {
	t.Helper()
	s := newStubSettings()
	l := NewTPMLimiter(s)
	t.Cleanup(l.Stop)
	return l, s
}

func TestTPMLimiter_NoCap(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	// tpm <= 0 means no cap: always admitted and no bucket is created.
	if !l.Allow("k", 0) {
		t.Fatal("tpm=0 should always allow")
	}
	if !l.Allow("k", -5) {
		t.Fatal("negative tpm should always allow")
	}
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("no bucket should be created for uncapped keys, got %d", n)
	}
}

func TestTPMLimiter_DrainAndReject(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 1000

	if !l.Allow("k", tpm) {
		t.Fatal("fresh budget should admit")
	}
	// Debit more than a full minute's budget to drive the budget clearly
	// negative, then admission must reject.
	l.Debit("k", 2*tpm)
	if l.Allow("k", tpm) {
		t.Fatal("exhausted budget should reject")
	}
}

func TestTPMLimiter_OverCapSingleDebit(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 100

	// A single response far larger than the minute budget must still register
	// as debt (debited in burst-sized chunks), blocking the next request.
	if !l.Allow("k", tpm) {
		t.Fatal("fresh budget should admit")
	}
	l.Debit("k", tpm*5)
	if l.Allow("k", tpm) {
		t.Fatal("over-cap debit should leave the budget exhausted")
	}
}

func TestTPMLimiter_PerKeyIsolation(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 500

	l.Allow("a", tpm)
	l.Debit("a", 2*tpm)
	if l.Allow("a", tpm) {
		t.Fatal("key a should be exhausted")
	}
	if !l.Allow("b", tpm) {
		t.Fatal("key b must be unaffected by key a's spend")
	}
}

func TestTPMLimiter_Refill(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	// 600 TPM = 10 tokens/sec. Drain to ~0, confirm reject, then a short wait
	// refills enough for one token.
	const tpm = 600

	l.Allow("k", tpm)
	l.Debit("k", tpm) // drains a full minute's budget to ~0
	if l.Allow("k", tpm) {
		t.Fatal("budget should be exhausted right after draining")
	}
	time.Sleep(200 * time.Millisecond) // 10/s * 0.2s = ~2 tokens
	if !l.Allow("k", tpm) {
		t.Fatal("budget should have refilled enough to admit")
	}
}

func TestTPMLimiter_DebitNoBucketIsNoop(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	// Debiting a key with no active bucket (no cap in effect) must not panic
	// or create a bucket.
	l.Debit("ghost", 1000)
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("Debit must not create a bucket, got %d", n)
	}
}

func TestTPMLimiter_TPMChangeReplacesBucket(t *testing.T) {
	l, _ := newTestTPMLimiter(t)

	l.Allow("k", 100)
	l.Debit("k", 500) // exhaust the 100-TPM bucket
	if l.Allow("k", 100) {
		t.Fatal("100-TPM bucket should be exhausted")
	}
	// Raising the key's TPM should replace the bucket with a fresh budget.
	if !l.Allow("k", 10000) {
		t.Fatal("changing TPM should reset the bucket and admit")
	}
}

func TestTPMLimiter_IdleEviction(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	l.Allow("k", 100)

	// Backdate the bucket past the idle cutoff and run cleanup directly.
	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()

	l.cleanup()

	l.mu.Lock()
	_, ok := l.buckets["k"]
	l.mu.Unlock()
	if ok {
		t.Fatal("idle bucket should have been evicted")
	}
}

// --- Middleware ---

// tpmReq builds a request carrying the virtual-key hash and (optionally) a
// per-key TPM override in its context, as ProxyKeyMiddleware would.
func tpmReq(keyHash string, perKeyTPM *int) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, keyHash)
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitTPMKey, perKeyTPM)
	return r.WithContext(ctx)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestTPMMiddleware_EnvDisabledIsNoop(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	tpm := 1
	l.Allow("k", tpm)
	l.Debit("k", 100) // exhaust
	// enabled=false (env kill-switch) → middleware must pass through regardless.
	h := l.Middleware(false)(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, tpmReq("k", &tpm))
	if rec.Code != http.StatusOK {
		t.Fatalf("env-disabled middleware should pass, got %d", rec.Code)
	}
}

func TestTPMMiddleware_DBDisabledIsNoop(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyEnabled, "false")
	tpm := 1
	l.Allow("k", tpm)
	l.Debit("k", 100)
	h := l.Middleware(true)(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, tpmReq("k", &tpm))
	if rec.Code != http.StatusOK {
		t.Fatalf("DB-disabled middleware should pass, got %d", rec.Code)
	}
}

func TestTPMMiddleware_NoCapPasses(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	h := l.Middleware(true)(okHandler())
	rec := httptest.NewRecorder()
	// No per-key TPM and no global default (stub returns default 0) → no cap.
	h.ServeHTTP(rec, tpmReq("k", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("uncapped request should pass, got %d", rec.Code)
	}
}

func TestTPMMiddleware_RejectsWhenExhausted(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	tpm := 600
	l.Allow("k", tpm)
	l.Debit("k", 2*tpm) // drive negative

	h := l.Middleware(true)(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, tpmReq("k", &tpm))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted budget should 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 response must set Retry-After")
	}
}

func TestTPMMiddleware_GlobalDefaultApplies(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyTPM, "600")

	h := l.Middleware(true)(okHandler())

	// First request (no per-key override) is admitted under the global default.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, tpmReq("k", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request under global default should pass, got %d", rec.Code)
	}

	// Exhaust the global-default budget, then the next request is rejected.
	l.Debit("k", 2*600)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, tpmReq("k", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("global-default budget exhaustion should 429, got %d", rec.Code)
	}
}

func TestTPMMiddleware_PerKeyOverridesGlobal(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyTPM, "1000000") // generous global default

	tpm := 600 // restrictive per-key override
	l.Allow("k", tpm)
	l.Debit("k", 2*tpm)

	h := l.Middleware(true)(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, tpmReq("k", &tpm))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("per-key cap should override the looser global default, got %d", rec.Code)
	}
}
