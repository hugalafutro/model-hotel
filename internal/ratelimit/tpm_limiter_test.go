package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// TestTPMRetryAfter covers the three return paths of tpmRetryAfter: a budget
// that already has a token (>=1s), a zero-rate limiter (defensive 1s), and a
// drained low-rate limiter where the wait rounds up to several seconds.
func TestTPMRetryAfter(t *testing.T) {
	t.Run("token available returns 1", func(t *testing.T) {
		lim := rate.NewLimiter(rate.Limit(10), 600) // fresh: full burst available
		if got := tpmRetryAfter(lim); got != 1 {
			t.Errorf("tpmRetryAfter(available) = %d, want 1", got)
		}
	})

	t.Run("zero rate and no tokens returns 1", func(t *testing.T) {
		lim := rate.NewLimiter(0, 0) // no tokens, Limit()==0 -> perSec<=0 guard
		if got := tpmRetryAfter(lim); got != 1 {
			t.Errorf("tpmRetryAfter(zero-rate) = %d, want 1", got)
		}
	})

	t.Run("drained low-rate limiter rounds up the wait", func(t *testing.T) {
		// 1 TPM => 1/60 token/sec, burst 60. Draining the full burst leaves ~0
		// tokens, so the wait to reach one token is ceil(1 / (1/60)) = 60s.
		lim := rate.NewLimiter(rate.Limit(1.0/60.0), 60)
		lim.ReserveN(time.Now(), 60)
		got := tpmRetryAfter(lim)
		if got < 2 {
			t.Errorf("tpmRetryAfter(drained) = %d, want a multi-second wait (>=2)", got)
		}
	})
}

// TestTPMLimiter_DebitNonPositiveIsNoop guards the tokens<=0 early return: a
// zero or negative token total must never touch the budget (a failed upstream
// call reports 0 tokens and must not be charged).
func TestTPMLimiter_DebitNonPositiveIsNoop(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	tpm := 600
	l.Allow("k", tpm) // create a bucket at full budget

	tokensOf := func() float64 {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.buckets["k"].limiter.Tokens()
	}
	before := tokensOf()

	l.Debit("k", 0)
	l.Debit("k", -100)

	// A real debit reserves tokens, reducing the count; a non-positive debit must
	// reserve nothing. The token count only ever rises (refill), so it must not
	// have dropped — this precisely catches a wrongful debit, unlike a bare Allow.
	if after := tokensOf(); after < before {
		t.Fatalf("non-positive Debit reduced the budget: before=%v after=%v", before, after)
	}
	if !l.Allow("k", tpm) {
		t.Fatal("non-positive Debit must leave the budget admitting requests")
	}
}

// TestTPMMiddleware_EmptyKeyPasses covers the keyHash=="" admission branch: a
// request with neither a virtual-key hash nor a RemoteAddr cannot be metered
// per key, so it passes through rather than 429ing.
func TestTPMMiddleware_EmptyKeyPasses(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyTPM, "600") // a global cap is set, yet the keyless req passes

	h := l.Middleware(true)(okHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "" // no key context + empty RemoteAddr => extractKey returns ""
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("keyless request should pass through, got %d", rec.Code)
	}
}

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

// ownedTPMReq builds a request carrying a virtual-key hash plus owner context
// (uid + user TPM cap), as ProxyKeyMiddleware would for an owned key.
func ownedTPMReq(keyHash, uid string, userTPM int) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(req.Context(), ctxkeys.VirtualKeyHashKey, keyHash)
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyOwnerIDKey, uid)
	ctx = context.WithValue(ctx, ctxkeys.UserRateLimitTPMKey, &userTPM)
	return req.WithContext(ctx)
}

// TestTPMMiddleware_UserAggregateBudget verifies that two keys owned by the
// same user share one aggregate budget: draining it through one key rejects
// the other key's next request even though neither key has a per-key cap.
func TestTPMMiddleware_UserAggregateBudget(t *testing.T) {
	l, _ := newTestTPMLimiter(t) // no global TPM: only the user stage is active
	h := l.Middleware(true)(okHandler())

	userTPM := 600
	// Admit key A: creates the user bucket and the A->user association.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-a", "uid-1", userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", rec.Code)
	}

	// Debit through key A far past the aggregate budget.
	l.Debit("key-a", userTPM*2)

	// Key B, same owner, must now be rejected by the user stage.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-b", "uid-1", userTPM))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same-owner key should hit the aggregate budget, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("user-stage 429 should carry Retry-After")
	}

	// A key owned by a different user is unaffected.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-c", "uid-2", userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("other owner's key should pass, got %d", rec.Code)
	}

	// An unowned key is unaffected too (no global cap configured).
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyHashKey, "key-plain"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unowned key should pass, got %d", rec.Code)
	}
}

// TestTPMLimiter_DebitDebitsOwnerBucket verifies the dual debit: the key's own
// bucket (when capped) and the owner's aggregate bucket both drop.
func TestTPMLimiter_DebitDebitsOwnerBucket(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyTPM, "1200") // per-key stage active via global default
	h := l.Middleware(true)(okHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-a", "uid-1", 600))
	if rec.Code != http.StatusOK {
		t.Fatalf("admission failed: %d", rec.Code)
	}

	tokensOf := func(bucket string) float64 {
		l.mu.Lock()
		defer l.mu.Unlock()
		e, ok := l.buckets[bucket]
		if !ok {
			t.Fatalf("bucket %q missing", bucket)
		}
		return e.limiter.Tokens()
	}
	keyBefore := tokensOf("key-a")
	userBefore := tokensOf("user:uid-1")

	l.Debit("key-a", 300)

	if after := tokensOf("key-a"); after >= keyBefore {
		t.Errorf("key bucket not debited: before=%v after=%v", keyBefore, after)
	}
	if after := tokensOf("user:uid-1"); after >= userBefore {
		t.Errorf("owner bucket not debited: before=%v after=%v", userBefore, after)
	}
}

// TestTPMLimiter_AssocClearedWhenOwnerRemoved verifies that once a key stops
// carrying owner context (ownership cleared at runtime), its next admission
// clears the stale association so Debit no longer charges the old owner.
func TestTPMLimiter_AssocClearedWhenOwnerRemoved(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyTPM, "1200")
	h := l.Middleware(true)(okHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-a", "uid-1", 600))
	if rec.Code != http.StatusOK {
		t.Fatalf("admission failed: %d", rec.Code)
	}

	// Re-admit without owner context: association must be cleared.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyHashKey, "key-a"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-admission failed: %d", rec.Code)
	}

	userTokens := func() float64 {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.buckets["user:uid-1"].limiter.Tokens()
	}
	before := userTokens()
	l.Debit("key-a", 300)
	if after := userTokens(); after < before {
		t.Errorf("stale association still debited the old owner: before=%v after=%v", before, after)
	}
}

func TestEffectiveTPM_FleetDivisor(t *testing.T) {
	// Global cap 600 across a 3-member fleet -> each member's effective cap 200.
	s := newStubSettings()
	s.set(settingsKeyTPM, "600")
	setFleetActive(s, 3)
	l := NewTPMLimiter(s)
	defer l.Stop()

	if got := l.effectiveTPM(context.Background()); got != 200 {
		t.Errorf("effectiveTPM = %d, want 200", got)
	}
}

func TestEffectiveTPM_FleetDivisorUnlimitedUntouched(t *testing.T) {
	// No global cap (0) stays 0 regardless of fleet size.
	s := newStubSettings()
	setFleetActive(s, 3)
	l := NewTPMLimiter(s)
	defer l.Stop()

	if got := l.effectiveTPM(context.Background()); got != 0 {
		t.Errorf("effectiveTPM = %d, want 0 (no cap)", got)
	}
}
