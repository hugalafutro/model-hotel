package ratelimit

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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
	tpmAdmit(t, l, "k", tpm) // create a bucket at full budget

	tokensOf := func() float64 {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.buckets["k"].limiter.Tokens()
	}
	before := tokensOf()

	l.Debit("k", "", 0)
	l.Debit("k", "", -100)

	// A real debit reserves tokens, reducing the count; a non-positive debit must
	// reserve nothing. The token count only ever rises (refill), so it must not
	// have dropped — this precisely catches a wrongful debit, unlike a bare admission check.
	if after := tokensOf(); after < before {
		t.Fatalf("non-positive Debit reduced the budget: before=%v after=%v", before, after)
	}
	if !tpmAdmit(t, l, "k", tpm) {
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

// tpmAdmit runs one request through the per-key TPM middleware, the path
// production admission actually takes, and reports whether it was admitted.
func tpmAdmit(t *testing.T, l *TPMLimiter, keyHash string, tpm int) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	l.Middleware(true)(okHandler()).ServeHTTP(rec, tpmReq(keyHash, &tpm))
	return rec.Code == http.StatusOK
}

func TestTPMLimiter_NoCap(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	// tpm <= 0 means no cap: always admitted and no bucket is created.
	if !tpmAdmit(t, l, "k", 0) {
		t.Fatal("tpm=0 should always allow")
	}
	if !tpmAdmit(t, l, "k", -5) {
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

	if !tpmAdmit(t, l, "k", tpm) {
		t.Fatal("fresh budget should admit")
	}
	// Debit more than a full minute's budget to drive the budget clearly
	// negative, then admission must reject.
	l.Debit("k", "", 2*tpm)
	if tpmAdmit(t, l, "k", tpm) {
		t.Fatal("exhausted budget should reject")
	}
}

func TestTPMLimiter_OverCapSingleDebit(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 100

	// A single response far larger than the minute budget must still register
	// as debt (debited in burst-sized chunks), blocking the next request.
	if !tpmAdmit(t, l, "k", tpm) {
		t.Fatal("fresh budget should admit")
	}
	l.Debit("k", "", tpm*5)
	if tpmAdmit(t, l, "k", tpm) {
		t.Fatal("over-cap debit should leave the budget exhausted")
	}
}

func TestTPMLimiter_PerKeyIsolation(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 500

	tpmAdmit(t, l, "a", tpm)
	l.Debit("a", "", 2*tpm)
	if tpmAdmit(t, l, "a", tpm) {
		t.Fatal("key a should be exhausted")
	}
	if !tpmAdmit(t, l, "b", tpm) {
		t.Fatal("key b must be unaffected by key a's spend")
	}
}

func TestTPMLimiter_Refill(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	// 600 TPM = 10 tokens/sec. Drain to ~0, confirm reject, then a short wait
	// refills enough for one token.
	const tpm = 600

	tpmAdmit(t, l, "k", tpm)
	l.Debit("k", "", tpm) // drains a full minute's budget to ~0
	if tpmAdmit(t, l, "k", tpm) {
		t.Fatal("budget should be exhausted right after draining")
	}
	time.Sleep(200 * time.Millisecond) // 10/s * 0.2s = ~2 tokens
	if !tpmAdmit(t, l, "k", tpm) {
		t.Fatal("budget should have refilled enough to admit")
	}
}

func TestTPMLimiter_DebitNoBucketIsNoop(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	// Debiting a key with no active bucket (no cap in effect) must not panic
	// or create a bucket.
	l.Debit("ghost", "", 1000)
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("Debit must not create a bucket, got %d", n)
	}
}

// TestTPMLimiter_DebitAfterEvictionStillCharges covers a request that outlives
// the idle sweep: a stream longer than the cutoff, on a key with no other
// traffic, has its bucket evicted between admission and completion. Dropping
// that debit would hand the key a whole minute's budget for free.
func TestTPMLimiter_DebitAfterEvictionStillCharges(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 500

	if !tpmAdmit(t, l, "k", tpm) {
		t.Fatal("fresh budget should admit")
	}
	// Age the bucket past the cutoff and sweep it, as the background cleanup
	// would while the request is still streaming.
	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()
	l.cleanup()
	l.mu.Lock()
	evicted := len(l.buckets)
	l.mu.Unlock()
	if evicted != 0 {
		t.Fatalf("sweep should have evicted the idle bucket, %d left", evicted)
	}

	l.Debit("k", "", 2*tpm)
	if tpmAdmit(t, l, "k", tpm) {
		t.Fatal("the debit must land on a rebuilt bucket and exhaust the budget")
	}
}

// TestTPMLimiter_OwnerDebitSurvivesEviction is the aggregate half of the same
// case. The sweep takes the key's assoc entry at the same cutoff as its bucket,
// so a debit landing after it has to find the owner through the cap memo, or
// the key is charged and the owner it belongs to is not.
func TestTPMLimiter_OwnerDebitSurvivesEviction(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	const tpm = 500
	owner := userBucketKey("owner-1")

	l.getEntry(context.Background(), "k", tpm)
	l.getEntry(context.Background(), owner, tpm)

	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.buckets[owner].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()
	l.cleanup()

	l.Debit("k", "owner-1", 2*tpm)

	l.mu.Lock()
	ownerEntry, ok := l.buckets[owner]
	l.mu.Unlock()
	if !ok {
		t.Fatal("the owner bucket should have been rebuilt to take the debit")
	}
	if ownerEntry.limiter.Tokens() > 0 {
		t.Errorf("owner budget = %v tokens, want it exhausted by the debit", ownerEntry.limiter.Tokens())
	}
}

// TestTPMLimiter_CapMemoIsSwept pins the bound on that memory: the memo outlives
// the bucket so a late debit can rebuild it, but not forever, or the map grows
// by one entry for every key the process ever admits.
func TestTPMLimiter_CapMemoIsSwept(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	l.getEntry(context.Background(), "k", 500)

	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()
	ageMemo(t, l, "k", minCapMemoTTL+time.Minute)
	l.cleanup()

	if memoLives(l, "k") {
		t.Error("a memo past its claimed horizon should be swept")
	}
	l.Debit("k", "", 10_000)
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Errorf("with no memo the debit must not rebuild a bucket, got %d", n)
	}
}

func TestTPMLimiter_TPMChangeReplacesBucket(t *testing.T) {
	l, _ := newTestTPMLimiter(t)

	tpmAdmit(t, l, "k", 100)
	l.Debit("k", "", 500) // exhaust the 100-TPM bucket
	if tpmAdmit(t, l, "k", 100) {
		t.Fatal("100-TPM bucket should be exhausted")
	}
	// Raising the key's TPM should replace the bucket with a fresh budget.
	if !tpmAdmit(t, l, "k", 10000) {
		t.Fatal("changing TPM should reset the bucket and admit")
	}
}

func TestTPMLimiter_IdleEviction(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	tpmAdmit(t, l, "k", 100)

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
	tpmAdmit(t, l, "k", tpm)
	l.Debit("k", "", 100) // exhaust
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
	tpmAdmit(t, l, "k", tpm)
	l.Debit("k", "", 100)
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
	tpmAdmit(t, l, "k", tpm)
	l.Debit("k", "", 2*tpm) // drive negative

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
	l.Debit("k", "", 2*600)
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
	tpmAdmit(t, l, "k", tpm)
	l.Debit("k", "", 2*tpm)

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
	l.Debit("key-a", "uid-1", userTPM*2)

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

// TestTPMMiddleware_RejectedPerKeyDoesNotDrainOwnerBucket guards against an
// owner-budget leak. When a request clears the user-aggregate stage but is
// then rejected by the per-key gate, the owner token it reserved must be
// returned. Otherwise sustained rejections on one over-cap key would drain
// the shared owner budget and 429 the owner's other keys, even though those
// rejected requests never run and so never call Debit.
func TestTPMMiddleware_RejectedPerKeyDoesNotDrainOwnerBucket(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	// Global default of 1 makes the per-key cap 1, so the per-key gate is the
	// bottleneck. The owner budget (userTPM) is larger.
	s.set(settingsKeyTPM, "1")
	userTPM := 5
	h := l.Middleware(true)(okHandler())

	// First request on key-a consumes its sole per-key token and one owner
	// token, and passes.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-a", "uid-1", userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", rec.Code)
	}

	// Hammer key-a past its per-key budget. Each request clears the owner
	// stage then fails the per-key gate. If the owner token were committed on
	// rejection, this loop would exhaust the shared owner budget.
	for i := 0; i < userTPM+3; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, ownedTPMReq("key-a", "uid-1", userTPM))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("over-cap key-a request %d should be 429, got %d", i, rec.Code)
		}
	}

	// Sibling key-b (same owner, its own per-key bucket) must still pass:
	// key-a's rejections must not have drained the shared owner budget.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-b", "uid-1", userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("sibling key-b should still pass (owner budget intact), got %d", rec.Code)
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

	l.Debit("key-a", "uid-1", 300)

	if after := tokensOf("key-a"); after >= keyBefore {
		t.Errorf("key bucket not debited: before=%v after=%v", keyBefore, after)
	}
	if after := tokensOf("user:uid-1"); after >= userBefore {
		t.Errorf("owner bucket not debited: before=%v after=%v", userBefore, after)
	}
}

// TestTPMLimiter_UnownedCompletionLeavesTheOwnerAlone verifies the other half
// of taking the owner from the completing request: a debit that carries no
// owner charges the key alone, so a request made while the key was unowned
// cannot land on whoever owned it before or after.
func TestTPMLimiter_UnownedCompletionLeavesTheOwnerAlone(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyTPM, "600")
	h := l.Middleware(true)(okHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ownedTPMReq("key-a", "uid-1", 600))
	if rec.Code != http.StatusOK {
		t.Fatalf("admission failed: %d", rec.Code)
	}

	userTokens := func() float64 {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.buckets["user:uid-1"].limiter.Tokens()
	}
	before := userTokens()
	l.Debit("key-a", "", 300)
	if after := userTokens(); after < before {
		t.Errorf("an unowned completion debited an owner: before=%v after=%v", before, after)
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

// TestTPMMiddleware_ConcurrentAdmissionReservesTokens guards the admission
// TOCTOU race: admission must atomically reserve a token, not merely peek at the
// available count. Under the old non-mutating Tokens() peek, many concurrent
// requests all observed tokens available before any of them reserved one, so far
// more than the per-minute burst passed and the budget was blown. okHandler
// returns immediately and never calls Debit, so nothing but the 1-token
// admission reservation can throttle: with a burst of tpm, at most tpm requests
// (plus a negligible sub-millisecond refill) may be admitted no matter how many
// fire at once.
func TestTPMMiddleware_ConcurrentAdmissionReservesTokens(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	tpm := 50 // burst == tpm == 50
	h := l.Middleware(true)(okHandler())

	const N = 200
	var admitted, rejected int64
	var wg sync.WaitGroup
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tpmReq("k", &tpm))
			switch rec.Code {
			case http.StatusOK:
				atomic.AddInt64(&admitted, 1)
			case http.StatusTooManyRequests:
				atomic.AddInt64(&rejected, 1)
			default:
				t.Errorf("unexpected status %d", rec.Code)
			}
		}()
	}
	wg.Wait()

	// The sub-millisecond run refills ~0 tokens, so admissions must stay within
	// the burst. The +2 slack absorbs any tiny refill without letting the old
	// peek-based regression (which would admit ~all N) slip through.
	if admitted > int64(tpm+2) {
		t.Fatalf("admitted %d of %d concurrent requests, want <= %d: admission did not reserve tokens atomically", admitted, N, tpm+2)
	}
	if admitted < 1 {
		t.Fatalf("admitted %d requests, want at least 1", admitted)
	}
	if admitted+rejected != N {
		t.Fatalf("accounting mismatch: admitted=%d rejected=%d, want sum %d", admitted, rejected, N)
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

// sessionTPMReq builds a request carrying only owner context and no virtual-key
// hash, as api.ChatUserContextMiddleware does on /api/chat/*.
func sessionTPMReq(uid string, userTPM *int) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/chat/chat", http.NoBody)
	ctx := context.WithValue(req.Context(), ctxkeys.VirtualKeyOwnerIDKey, uid)
	if userTPM != nil {
		ctx = context.WithValue(ctx, ctxkeys.UserRateLimitTPMKey, userTPM)
	}
	return req.WithContext(ctx)
}

// The keyless surface's budget is the owner's, drained by DebitUser (its only
// debit path: Debit is keyed by virtual-key hash, which this surface has none
// of) and refused by UserMiddleware once it is empty.
func TestTPMUserMiddleware_OwnerBudgetIsEnforcedWithoutAKey(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	h := l.UserMiddleware(true)(okHandler())

	userTPM := 600
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", rec.Code)
	}

	l.DebitUser("uid-1", userTPM*2)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("drained owner budget should reject, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("user-stage 429 should carry Retry-After")
	}

	// Another account is untouched: the debit lands on one owner's bucket only.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, sessionTPMReq("uid-2", &userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("other owner should pass, got %d", rec.Code)
	}
}

// DebitUser and Debit must land on the same bucket, or a user's /v1 spend and
// their dashboard chat spend would be metered separately and neither cap would
// hold. Draining through the keyed path rejects the keyless one.
func TestTPMUserMiddleware_SharesTheBucketWithKeyedTraffic(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	userTPM := 600

	// Admit an owned key on the /v1 middleware: creates the owner bucket and
	// the owner the completion hands back to Debit.
	rec := httptest.NewRecorder()
	l.Middleware(true)(okHandler()).ServeHTTP(rec, ownedTPMReq("key-a", "uid-9", userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("keyed request should pass, got %d", rec.Code)
	}
	l.Debit("key-a", "uid-9", userTPM*2)

	rec = httptest.NewRecorder()
	l.UserMiddleware(true)(okHandler()).ServeHTTP(rec, sessionTPMReq("uid-9", &userTPM))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("chat request should hit the budget its owner's key drained, got %d", rec.Code)
	}
}

// Everything that means "nothing to enforce here" must pass through untouched.
// User-level caps have no global-settings fallback, so an account without one
// is not silently handed the global TPM the way an unowned key is.
func TestTPMUserMiddleware_PassesThroughWhenThereIsNothingToEnforce(t *testing.T) {
	zero := 0
	tpm := 600
	cases := []struct {
		name    string
		enabled bool
		req     *http.Request
	}{
		{"env kill-switch off", false, sessionTPMReq("uid-1", &tpm)},
		{"no owner in context", true, httptest.NewRequest(http.MethodPost, "/api/chat/chat", http.NoBody)},
		{"owner has no tpm cap", true, sessionTPMReq("uid-1", nil)},
		{"owner cap is zero", true, sessionTPMReq("uid-1", &zero)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, s := newTestTPMLimiter(t)
			// A global default must not leak into the user stage.
			s.set(settingsKeyTPM, "1")
			rec := httptest.NewRecorder()
			l.UserMiddleware(tc.enabled)(okHandler()).ServeHTTP(rec, tc.req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
		})
	}
}

// The runtime toggle is shared with the rest of the rate limiter: switching
// rate limiting off in settings must stop this stage too, or an operator's
// kill-switch would leave the chat surface throttled.
func TestTPMUserMiddleware_RuntimeToggleOff(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	userTPM := 600

	// Admission creates the bucket, so drain it while limiting is still on.
	rec := httptest.NewRecorder()
	l.UserMiddleware(true)(okHandler()).ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))
	l.DebitUser("uid-1", userTPM*4)

	s.set(settingsKeyEnabled, "false")
	rec = httptest.NewRecorder()
	l.UserMiddleware(true)(okHandler()).ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with rate limiting toggled off", rec.Code)
	}
}

// DebitUser ignores the inputs that cannot name a bucket rather than charging
// "user:" (which every capless caller would then share).
func TestTPMLimiter_DebitUserIgnoresNonBuckets(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	userTPM := 600
	rec := httptest.NewRecorder()
	l.UserMiddleware(true)(okHandler()).ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))

	l.DebitUser("", userTPM*4)
	l.DebitUser("uid-1", 0)
	l.DebitUser("uid-1", -5)

	rec = httptest.NewRecorder()
	l.UserMiddleware(true)(okHandler()).ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: no-op debits must not drain the budget", rec.Code)
	}
}

// TestCapMemoTTL covers the horizon arithmetic on its own: every ordinary
// request_timeout lands on the floor, a long one scales past it, a missing or
// nonsensical one falls back to the floor, and one too large to scale saturates
// rather than wrapping and collapsing onto a floor far shorter than the request
// it has to outlast.
func TestCapMemoTTL(t *testing.T) {
	cases := []struct {
		name           string
		requestTimeout time.Duration
		want           time.Duration
	}{
		{"default one minute floors at a day", time.Minute, minCapMemoTTL},
		{"half an hour still floors at a day", 30 * time.Minute, minCapMemoTTL},
		{"the crossover derives exactly the floor", 36 * time.Minute, minCapMemoTTL},
		{"a minute past the crossover scales", 37 * time.Minute, 24*time.Hour + 40*time.Minute},
		{"an hour scales past the floor", time.Hour, 40 * time.Hour},
		{"three hours scales further", 3 * time.Hour, 120 * time.Hour},
		{"zero falls back to the floor", 0, minCapMemoTTL},
		{"negative falls back to the floor", -time.Hour, minCapMemoTTL},
		{"the largest scalable timeout still scales", math.MaxInt64 / capMemoTimeoutFactor, (math.MaxInt64 / capMemoTimeoutFactor) * capMemoTimeoutFactor},
		{"one tick past that saturates", math.MaxInt64/capMemoTimeoutFactor + 1, time.Duration(math.MaxInt64)},
		{"an overflowing timeout saturates", time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capMemoTTL(tc.requestTimeout); got != tc.want {
				t.Errorf("capMemoTTL(%v) = %v, want %v", tc.requestTimeout, got, tc.want)
			}
		})
	}
}

// TestTPMLimiter_CapMemoHorizonFollowsRequestTimeout is the wiring half: a memo
// older than the floor but younger than the horizon a raised request_timeout
// implies must survive the sweep, and still take a late debit. A horizon that
// ignored the setting would sweep this memo and drop the debit, which is what
// the memo exists to prevent.
func TestTPMLimiter_CapMemoHorizonFollowsRequestTimeout(t *testing.T) {
	const tpm = 500
	l, s := newTestTPMLimiter(t)
	// Forty times one hour is well past the one-day floor, so a memo aged 25
	// hours is inside the horizon here and outside it at the default timeout.
	s.set(settingsKeyRequestTimeout, "1h")
	l.getEntry(context.Background(), "k", tpm)

	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()
	ageMemo(t, l, "k", 25*time.Hour)
	l.cleanup()

	l.mu.Lock()
	buckets := len(l.buckets)
	l.mu.Unlock()
	if !memoLives(l, "k") {
		t.Fatal("a memo inside the horizon a raised request_timeout implies must survive the sweep")
	}
	if buckets != 0 {
		t.Fatalf("the idle bucket should still be evicted, got %d", buckets)
	}

	l.Debit("k", "", 2*tpm)
	l.mu.Lock()
	rebuilt, ok := l.buckets["k"]
	l.mu.Unlock()
	if !ok {
		t.Fatal("the surviving memo must let the late debit rebuild the bucket")
	}
	if got := rebuilt.limiter.Tokens(); got > 0 {
		t.Errorf("the debit must land on the rebuilt bucket, tokens left = %v", got)
	}
}

// TestTPMLimiter_CapMemoSweptAtTheDefaultTimeout is the control for the case
// above: the same 25-hour-old memo is swept when request_timeout is the default,
// so the test above proves the setting moved the horizon and not that the sweep
// stopped working.
func TestTPMLimiter_CapMemoSweptAtTheDefaultTimeout(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	l.getEntry(context.Background(), "k", 500)

	ageMemo(t, l, "k", 25*time.Hour)
	l.cleanup()

	if memoLives(l, "k") {
		t.Error("at the default request_timeout a memo past the one-day floor must be swept")
	}
}

// TestTPMLimiter_CapMemoHorizonSurvivesALoweredTimeout pins the lowering
// direction: the horizon a memo claimed has to outlast a request admitted
// against it even when the operator lowers request_timeout while that request is
// still running. A horizon resolved at sweep time would shrink underneath the
// in-flight request and drop its debit, which is what the memo exists to
// prevent.
func TestTPMLimiter_CapMemoHorizonSurvivesALoweredTimeout(t *testing.T) {
	const tpm = 500
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "1h")
	l.getEntry(context.Background(), "k", tpm)

	// The operator drops the timeout back to the default after admission.
	s.set(settingsKeyRequestTimeout, "1m")

	// The bucket is aged past the idle cutoff as well, so it is genuinely gone
	// after the sweep and the rebuild below can only come from the memo.
	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()
	ageMemo(t, l, "k", 25*time.Hour)
	l.cleanup()

	if !memoLives(l, "k") {
		t.Fatal("lowering request_timeout must not sweep a memo written under the longer one")
	}
	l.mu.Lock()
	buckets := len(l.buckets)
	l.mu.Unlock()
	if buckets != 0 {
		t.Fatalf("the idle bucket should have been evicted, got %d", buckets)
	}

	l.Debit("k", "", 2*tpm)
	l.mu.Lock()
	_, rebuilt := l.buckets["k"]
	l.mu.Unlock()
	if !rebuilt {
		t.Error("the surviving memo must still let the late debit rebuild the bucket")
	}
}

// TestTPMLimiter_CapMemoHorizonWithUnparseableTimeout covers the fallback
// through the real path rather than the pure function: a request_timeout the
// settings layer cannot parse reads as the proxy default, so the memo gets the
// floor and is swept past it.
func TestTPMLimiter_CapMemoHorizonWithUnparseableTimeout(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "not a duration")
	l.getEntry(context.Background(), "k", 500)

	if got := remainingMemoHorizon(t, l, "k"); got != minCapMemoTTL {
		t.Fatalf("an unparseable request_timeout should derive the floor, got %v", got)
	}
	ageMemo(t, l, "k", minCapMemoTTL+time.Minute)
	l.cleanup()

	if memoLives(l, "k") {
		t.Error("a memo past the floor should be swept when the timeout is unparseable")
	}
}

// TestTPMLimiter_CapMemoDeadlineOnlyMovesOut covers the rule that makes the
// lowering case above safe: a later admission under a longer request_timeout
// pushes the memo's deadline out, and one under a shorter timeout leaves it
// where it is. Assigning the new deadline unconditionally would pull it back in
// and strand the request admitted under the longer setting.
func TestTPMLimiter_CapMemoDeadlineOnlyMovesOut(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	ctx := context.Background()

	s.set(settingsKeyRequestTimeout, "1h")
	l.getEntry(ctx, "k", 500)
	if got := remainingMemoHorizon(t, l, "k"); got != 40*time.Hour {
		t.Fatalf("a one hour timeout should claim a 40 hour horizon, got %v", got)
	}

	s.set(settingsKeyRequestTimeout, "4h")
	l.getEntry(ctx, "k", 500)
	if got := remainingMemoHorizon(t, l, "k"); got != 160*time.Hour {
		t.Errorf("a longer timeout should push the deadline out to 160h, got %v", got)
	}

	s.set(settingsKeyRequestTimeout, "1m")
	l.getEntry(ctx, "k", 500)
	if got := remainingMemoHorizon(t, l, "k"); got != 160*time.Hour {
		t.Errorf("a shorter timeout must not pull the deadline back in, got %v", got)
	}
}

// TestTPMLimiter_CapMemoDeadlineRestampsAtTheFloor is the other half of that
// rule. The deadline never descends, but it is an absolute time rather than a
// duration that ratchets, so a long timeout's claim runs out on its own and
// ordinary admissions re-stamp the memo at the floor from then on. A horizon
// held as a duration would instead pin a busy key for as long as the process
// runs.
func TestTPMLimiter_CapMemoDeadlineRestampsAtTheFloor(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	ctx := context.Background()

	s.set(settingsKeyRequestTimeout, "1h")
	l.getEntry(ctx, "k", 500)

	// The long request finishes and the operator restores the default. Winding
	// the deadline back 20 hours stands in for that time passing, leaving the
	// memo still live with 20 hours of its 40 to run, so what follows is an
	// ordinary admission against a healthy memo rather than a revival. The
	// deadline is never pulled in. Once the old claim has less than a floor's
	// worth left to run, an ordinary admission re-stamps it at the floor, and
	// that is as far as it goes from then on.
	s.set(settingsKeyRequestTimeout, "1m")
	ageMemo(t, l, "k", 20*time.Hour)

	l.getEntry(ctx, "k", 500)
	if got := remainingMemoHorizon(t, l, "k"); got != minCapMemoTTL {
		t.Errorf("a later admission should carry the memo on the floor again, got %v", got)
	}
}

// TestTPMLimiter_CapMemoHorizonThroughTheMiddleware pins the wiring the direct
// getEntry tests cannot see: the admission path itself has to carry a context
// the settings read can resolve, or every memo would claim the floor whatever
// request_timeout says.
func TestTPMLimiter_CapMemoHorizonThroughTheMiddleware(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "1h")

	if !tpmAdmit(t, l, "k", 500) {
		t.Fatal("the first request under a fresh budget should be admitted")
	}
	if got := remainingMemoHorizon(t, l, "k"); got != 40*time.Hour {
		t.Errorf("admission should claim the horizon the live setting implies, got %v", got)
	}

	// A second request on the same key finds a warm bucket. The horizon still has
	// to be re-claimed there, or a key busy since before the setting was raised
	// would keep carrying the shorter one.
	s.set(settingsKeyRequestTimeout, "4h")
	if !tpmAdmit(t, l, "k", 500) {
		t.Fatal("the second request should still be inside the budget")
	}
	if got := remainingMemoHorizon(t, l, "k"); got != 160*time.Hour {
		t.Errorf("an admission on a warm bucket should re-claim the horizon, got %v", got)
	}
}

// TestTPMLimiter_CapMemoSaturatingTimeoutSurvivesTheSweep drives a
// request_timeout past the point where the scaling would overflow all the way
// through admission and a sweep. Without the saturation guard the product wraps,
// the horizon collapses onto the floor, and this memo is swept while a request
// under that timeout could still be running.
func TestTPMLimiter_CapMemoSaturatingTimeoutSurvivesTheSweep(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "100000h") // past math.MaxInt64 / 40

	l.getEntry(context.Background(), "k", 500)
	ageMemo(t, l, "k", 30*24*time.Hour)
	l.cleanup()

	if !memoLives(l, "k") {
		t.Error("a memo under a saturating request_timeout must outlive a month-long sweep")
	}
}

// TestTPMLimiter_DebitDoesNotExtendTheMemoDeadline pins the other half of that
// rule: only admissions claim a horizon. A debit closes the request that already
// claimed one, so refreshing the deadline there would hold every memo for a
// further horizon past the last request that needed it.
func TestTPMLimiter_DebitDoesNotExtendTheMemoDeadline(t *testing.T) {
	const tpm = 500
	l, _ := newTestTPMLimiter(t)
	l.getEntry(context.Background(), "k", tpm)

	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	before := l.caps["k"].expiresAt
	l.mu.Unlock()
	l.cleanup()

	l.mu.Lock()
	buckets := len(l.buckets)
	l.mu.Unlock()
	if buckets != 0 {
		t.Fatalf("the idle bucket should have been evicted, got %d", buckets)
	}

	l.Debit("k", "", 2*tpm)
	l.mu.Lock()
	after := l.caps["k"].expiresAt
	l.mu.Unlock()
	if !after.Equal(before) {
		t.Errorf("a debit must leave the memo deadline alone: was %v, now %v", before, after)
	}
}

// TestTPMLimiter_CapMemoHorizonIgnoresRequestCancellation pins the detached
// read: a client that disconnects during admission does not stop the proxy
// finishing the upstream call and debiting it, so the horizon has to come from
// the setting even when the request's own context is already gone. Resolving it
// under that context would claim the floor and sweep the memo out from under a
// request entitled to far longer.
func TestTPMLimiter_CapMemoHorizonIgnoresRequestCancellation(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "1h")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.getEntry(ctx, "k", 500)

	if got := remainingMemoHorizon(t, l, "k"); got != 40*time.Hour {
		t.Errorf("a cancelled request must still claim the horizon the setting implies, got %v", got)
	}
}

// TestTPMLimiter_RejectedRequestStillClaimsTheHorizon pins what the memo comment
// asserts: the claim is made at admission, before the budget decides, so a
// request turned away with a 429 has still pushed the deadline out. Claiming it
// only for admitted requests would leave a key whose budget is exhausted
// carrying a deadline nobody refreshes.
func TestTPMLimiter_RejectedRequestStillClaimsTheHorizon(t *testing.T) {
	const tpm = 60
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "1h")

	if !tpmAdmit(t, l, "k", tpm) {
		t.Fatal("the first request under a fresh budget should be admitted")
	}
	l.Debit("k", "", 10*tpm) // drive the budget well past empty
	ageMemo(t, l, "k", 10*time.Hour)
	before := remainingMemoHorizon(t, l, "k")

	if tpmAdmit(t, l, "k", tpm) {
		t.Fatal("the budget is exhausted, so the next request must be rejected")
	}
	if got := remainingMemoHorizon(t, l, "k"); got != 40*time.Hour {
		t.Errorf("a rejected request should still claim the full horizon, was %v, got %v", before, got)
	}
}

// TestTPMLimiter_HorizonReadCarriesItsOwnDeadline pins the bound on the detached
// read. Detaching drops the caller's deadline and the settings repository sets
// none of its own, so without a bound here a database that has stopped
// answering would hold up admission indefinitely. Asserting the deadline the
// read is handed proves the bound without waiting for it to expire.
func TestTPMLimiter_HorizonReadCarriesItsOwnDeadline(t *testing.T) {
	s := newStubSettings()
	l := NewTPMLimiter(&deadlineSpy{SettingsReader: s})
	t.Cleanup(l.Stop)

	l.memoHorizon(context.Background())

	spy, ok := l.settings.(*deadlineSpy)
	if !ok {
		t.Fatal("the limiter should still hold the spy")
	}
	// Measured after the deadline was set, so a budget can only read shorter than
	// its bound and no amount of stalling can push it over. An unbounded or
	// wildly longer deadline is what this catches.
	shortest, _, seen := spy.budgets()
	if !seen {
		t.Fatal("the horizon read must carry a deadline of its own")
	}
	if shortest > settingsReadTimeout {
		t.Errorf("the read's deadline should be no more than %v out, got %v", settingsReadTimeout, shortest)
	}
}

// TestTPMLimiter_SweepReadCarriesTheLongerBound is the same guard for the sweep,
// which reads off the request path and so waits longer. It still has to carry a
// bound: without one a hung settings store would hold the sweep goroutine, and
// with it the limiter's eviction, for as long as the process runs.
func TestTPMLimiter_SweepReadCarriesTheLongerBound(t *testing.T) {
	l := NewTPMLimiter(&deadlineSpy{SettingsReader: newStubSettings()})
	t.Cleanup(l.Stop)

	l.sweep()

	spy, ok := l.settings.(*deadlineSpy)
	if !ok {
		t.Fatal("the limiter should still hold the spy")
	}
	_, longest, seen := spy.budgets()
	if !seen {
		t.Fatal("the sweep's read must carry a deadline of its own")
	}
	if longest > markRefreshTimeout {
		t.Errorf("the sweep's deadline should be no more than %v out, got %v", markRefreshTimeout, longest)
	}
	if longest <= settingsReadTimeout {
		t.Errorf("the sweep should wait longer than an admission's %v, got %v", settingsReadTimeout, longest)
	}
}

// TestTPMLimiter_HorizonReadFallsBackWhenSettingsHang is what that bound buys:
// a settings read that never answers has to end in the default horizon and let
// admission carry on, rather than holding the request until the client gives up.
func TestTPMLimiter_HorizonReadFallsBackWhenSettingsHang(t *testing.T) {
	l := NewTPMLimiter(hangingSettings{SettingsReader: newStubSettings()})
	t.Cleanup(l.Stop)

	// Returning at all is half the assertion: the stub answers only once the
	// deadline the read carries has passed, so without that deadline this hangs
	// until the test binary times out.
	l.getEntry(context.Background(), "k", 500)

	if got := remainingMemoHorizon(t, l, "k"); got != minCapMemoTTL {
		t.Errorf("a settings read that hangs should derive the floor, got %v", got)
	}
}

// TestTPMLimiter_HungReadKeepsTheLastKnownHorizon covers the fallback that makes
// a hanging database survivable: a gateway running a long request_timeout keeps
// the horizon its last completed read produced, instead of dropping to the floor
// and sweeping memos out from under the requests it is still admitting.
func TestTPMLimiter_HungReadKeepsTheLastKnownHorizon(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRequestTimeout, "1h")
	hang := &switchableSettings{SettingsReader: stub}
	l := NewTPMLimiter(hang)
	t.Cleanup(l.Stop)

	if got := l.memoHorizon(context.Background()); got != 40*time.Hour {
		t.Fatalf("the first read should derive the setting's horizon, got %v", got)
	}

	hang.hang.Store(true)
	if got := l.memoHorizon(context.Background()); got != 40*time.Hour {
		t.Fatalf("a hung read should keep the last known horizon, got %v", got)
	}

	// What that horizon is for: a memo claimed while the database is hanging has
	// to outlive the sweep by as long as the gateway's real request_timeout
	// implies, not by the default's day.
	l.getEntry(context.Background(), "k", 500)
	ageMemo(t, l, "k", minCapMemoTTL+time.Hour)
	l.cleanup()
	if !memoLives(l, "k") {
		t.Error("a memo claimed during the hang should survive past the default's floor")
	}
}

// TestTPMLimiter_ClaimsAndSweepsRace runs admissions against the sweeper on one
// key, which is what production does on every sweep tick. Under the race detector
// this is the check that the memo's deadline is claimed and read under the same
// lock the sweep takes.
func TestTPMLimiter_ClaimsAndSweepsRace(t *testing.T) {
	l, _ := newTestTPMLimiter(t)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				l.getEntry(context.Background(), "k", 500)
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				l.cleanup()
			}
		}()
	}

	// Sampled alongside them, because the deadline only ever moving out is the
	// invariant the whole design rests on and a lost update would break it.
	var last time.Time
	var slipped bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			l.mu.Lock()
			memo, ok := l.caps["k"]
			var seen time.Time
			if ok {
				seen = memo.expiresAt
			}
			l.mu.Unlock()
			if ok {
				if seen.Before(last) {
					slipped = true
				}
				last = seen
			}
		}
	}()
	wg.Wait()

	if slipped {
		t.Error("a memo deadline moved backwards while admissions and sweeps raced")
	}
	if !memoLives(l, "k") {
		t.Error("a key admitting throughout should still hold its memo")
	}
}

// TestTPMLimiter_SweepRefreshesTheHorizonMark covers the path that keeps the
// fallback usable on a store that is slow rather than broken. An admission's own
// read gives up in milliseconds, so if nothing read request_timeout with a
// longer bound the mark could never rise above the default's floor.
func TestTPMLimiter_SweepRefreshesTheHorizonMark(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "4h")

	// Aged past the floor but well inside what a four hour timeout implies, so
	// the sweep has to keep it and evict the idle bucket beside it.
	l.getEntry(context.Background(), "k", 500)
	ageMemo(t, l, "k", minCapMemoTTL+time.Hour)
	l.mu.Lock()
	l.buckets["k"].lastUsed = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()
	// That admission recorded the mark on its way through, so clear it: the
	// assertion below is about the sweep's own read and would pass without one.
	l.lastGoodHorizon.Store(0)

	l.sweep()

	if got := time.Duration(l.lastGoodHorizon.Load()); got != 160*time.Hour {
		t.Errorf("the sweep should record the horizon the setting implies, got %v", got)
	}
	if !memoLives(l, "k") {
		t.Error("the sweep should keep a memo still inside its claimed horizon")
	}
	l.mu.Lock()
	buckets := len(l.buckets)
	l.mu.Unlock()
	if buckets != 0 {
		t.Errorf("the sweep should still evict the idle bucket, got %d", buckets)
	}
}

// TestTPMLimiter_ReadHorizonReportsATimeout covers the shared read giving up on
// a store that will not answer. It runs against a bound of its own rather than
// either caller's, so the case is pinned without waiting out the seconds the
// sweep is willing to spend.
func TestTPMLimiter_ReadHorizonReportsATimeout(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRequestTimeout, "4h")
	l := NewTPMLimiter(hangingSettings{SettingsReader: stub})
	t.Cleanup(l.Stop)

	horizon, timedOut := l.readHorizon(context.Background(), time.Millisecond)
	if !timedOut {
		t.Error("a read that never answers should report a timeout")
	}
	if horizon != minCapMemoTTL {
		t.Errorf("a read that never answers derives the default's horizon, got %v", horizon)
	}
}

// TestTPMLimiter_LateAnswerIsNotTreatedAsATimeout pins the branch that tells a
// read which answered from one which gave up. The deadline can fire in the
// moment after the value comes back, and a lowered request_timeout has to be
// honoured even then.
func TestTPMLimiter_LateAnswerIsNotTreatedAsATimeout(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRequestTimeout, "4h")
	l := NewTPMLimiter(lateAnswerSettings{SettingsReader: stub})
	t.Cleanup(l.Stop)
	l.rememberHorizon(500 * time.Hour)

	if got := l.memoHorizon(context.Background()); got != 160*time.Hour {
		t.Errorf("a value that came from the setting should stand, got %v", got)
	}
}

// TestTPMLimiter_LateFloorAnswerReadsAsATimeout pins the case the predicate
// cannot separate, so that nobody simplifies it away believing it does. A
// setting that genuinely derives the floor, answered as the deadline passes,
// looks exactly like a read that gave up, and the remembered horizon wins.
// Nothing can tell the two apart from the value alone, and preferring the longer
// one lengthens retention rather than dropping a debit.
func TestTPMLimiter_LateFloorAnswerReadsAsATimeout(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRequestTimeout, "1m")
	l := NewTPMLimiter(lateAnswerSettings{SettingsReader: stub})
	t.Cleanup(l.Stop)
	l.rememberHorizon(160 * time.Hour)

	if got := l.memoHorizon(context.Background()); got != 160*time.Hour {
		t.Errorf("a late floor answer should fall back to the mark, got %v", got)
	}
}

// lateAnswerSettings answers with the real value but only once the read's own
// deadline has passed, the race the branch above exists for.
type lateAnswerSettings struct {
	SettingsReader
}

func (a lateAnswerSettings) GetDuration(ctx context.Context, key string, def time.Duration) time.Duration {
	got := a.SettingsReader.GetDuration(ctx, key, def)
	<-ctx.Done()
	return got
}

// TestTPMLimiter_TimeoutPrefersTheMarkOverALoweredSetting is why the mark
// exists. Once the store stops answering, the horizon has to come from what the
// gateway was running, not from the default a timed-out read hands back, even
// though the setting itself has since been lowered.
func TestTPMLimiter_TimeoutPrefersTheMarkOverALoweredSetting(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRequestTimeout, "4h")
	settings := &switchableSettings{SettingsReader: stub}
	l := NewTPMLimiter(settings)
	t.Cleanup(l.Stop)

	if got := l.memoHorizon(context.Background()); got != 160*time.Hour {
		t.Fatalf("the first read should derive the setting's horizon, got %v", got)
	}

	stub.set(settingsKeyRequestTimeout, "1m")
	settings.hang.Store(true)
	if got := l.memoHorizon(context.Background()); got != 160*time.Hour {
		t.Errorf("a timed-out read should fall back to the mark, not the floor, got %v", got)
	}
}

// TestRememberHorizon covers the high-water mark under contention: a mark can
// only climb, so racing writers below it change nothing and the highest wins.
func TestRememberHorizon(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	l.rememberHorizon(100 * time.Hour)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One writer is above the mark, the rest below it.
			if i == 7 {
				l.rememberHorizon(200 * time.Hour)
				return
			}
			l.rememberHorizon(time.Duration(i) * time.Hour)
		}()
	}
	wg.Wait()

	if got := time.Duration(l.lastGoodHorizon.Load()); got != 200*time.Hour {
		t.Errorf("the mark should hold the highest horizon offered, got %v", got)
	}

	// A saturating horizon is the one value the mark refuses, since carrying it
	// would leave every later timed-out read claiming centuries on every key.
	l.rememberHorizon(time.Duration(math.MaxInt64))
	if got := time.Duration(l.lastGoodHorizon.Load()); got != 200*time.Hour {
		t.Errorf("a saturating horizon should not be remembered, got %v", got)
	}
}

// TestTPMLimiter_CompletedReadWinsOverTheRememberedHorizon is the anti-pin rule:
// the remembered horizon exists for reads that time out, so lowering
// request_timeout has to take effect immediately even though a longer horizon is
// on record. Taking the longer of the two unconditionally would strand the
// setting at its highest value for the life of the process.
func TestTPMLimiter_CompletedReadWinsOverTheRememberedHorizon(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	ctx := context.Background()

	s.set(settingsKeyRequestTimeout, "1h")
	if got := l.memoHorizon(ctx); got != 40*time.Hour {
		t.Fatalf("the first read should derive the setting's horizon, got %v", got)
	}

	s.set(settingsKeyRequestTimeout, "1m")
	if got := l.memoHorizon(ctx); got != minCapMemoTTL {
		t.Errorf("a completed read should return the lowered setting's horizon, got %v", got)
	}
}

// TestTPMLimiter_RememberedHorizonSurvivesADefaultingRead covers the other half.
// A read that comes back with the default, whether the key is unset, the value
// is unusable, or the repository failed, is indistinguishable from any other,
// and letting it overwrite the mark would erase the long horizon exactly when a
// later timeout needs it.
func TestTPMLimiter_RememberedHorizonSurvivesADefaultingRead(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRequestTimeout, "1h")
	settings := &switchableSettings{SettingsReader: stub}
	l := NewTPMLimiter(settings)
	t.Cleanup(l.Stop)

	if got := l.memoHorizon(context.Background()); got != 40*time.Hour {
		t.Fatalf("the first read should derive the setting's horizon, got %v", got)
	}

	// A read that fails fast looks exactly like an unset key.
	stub.set(settingsKeyRequestTimeout, "")
	if got := l.memoHorizon(context.Background()); got != minCapMemoTTL {
		t.Fatalf("a failed read reads as the default, got %v", got)
	}

	settings.hang.Store(true)
	if got := l.memoHorizon(context.Background()); got != 40*time.Hour {
		t.Error("the failed read should not have erased the horizon a timeout falls back to")
	}
}

// TestWarnedSlowRead pins the rate limit on the slow-read warning: one caller
// per interval speaks and the rest stay quiet, including when they arrive at
// once, or a hanging database would put a line in the log for every admission it
// stalls.
func TestWarnedSlowRead(t *testing.T) {
	l, _ := newTestTPMLimiter(t)

	if !l.warnedSlowRead() {
		t.Fatal("the first slow read in an interval should warn")
	}
	if l.warnedSlowRead() {
		t.Error("a second slow read inside the interval should stay quiet")
	}

	l.lastSlowReadWarn.Store(time.Now().Add(-slowReadWarnInterval - time.Second).UnixNano())
	if !l.warnedSlowRead() {
		t.Error("once the interval has passed the next slow read should warn again")
	}

	// A hung database stalls every admission at once, so the compare-and-swap
	// has to hold when they all arrive together and not only in sequence.
	l.lastSlowReadWarn.Store(time.Now().Add(-slowReadWarnInterval - time.Second).UnixNano())
	var spoke atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.warnedSlowRead() {
				spoke.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := spoke.Load(); got != 1 {
		t.Errorf("exactly one racing caller should warn, got %d", got)
	}

	// A wall clock stepped backwards over the stamp leaves the gap negative.
	// Reading that as "not yet due" would silence the warning until the clock
	// caught up with itself.
	l.lastSlowReadWarn.Store(time.Now().Add(time.Hour).UnixNano())
	if !l.warnedSlowRead() {
		t.Error("a stamp in the future should not suppress the warning")
	}
}

// switchableSettings answers normally until hang is set, after which the read
// returns only once its own deadline has passed.
type switchableSettings struct {
	SettingsReader
	hang atomic.Bool
}

func (s *switchableSettings) GetDuration(ctx context.Context, key string, def time.Duration) time.Duration {
	if s.hang.Load() {
		<-ctx.Done()
		return def
	}
	return s.SettingsReader.GetDuration(ctx, key, def)
}

// TestTPMLimiter_AdmissionSurvivesAHangingStore is the contract the bound exists
// for, seen from outside: a request still gets an answer while the settings
// store is refusing to, rather than being held until the client gives up.
func TestTPMLimiter_AdmissionSurvivesAHangingStore(t *testing.T) {
	l := NewTPMLimiter(hangingSettings{SettingsReader: newStubSettings()})
	t.Cleanup(l.Stop)

	if !tpmAdmit(t, l, "k", 500) {
		t.Fatal("a request should still be admitted while the settings store hangs")
	}
	if got := remainingMemoHorizon(t, l, "k"); got != minCapMemoTTL {
		t.Errorf("with nothing known the claim should be the floor, got %v", got)
	}
}

// hangingSettings stands in for a database that has stopped answering: the read
// returns only once its own deadline has passed.
type hangingSettings struct {
	SettingsReader
}

func (h hangingSettings) GetDuration(ctx context.Context, _ string, def time.Duration) time.Duration {
	<-ctx.Done()
	return def
}

// deadlineSpy records how much time each horizon read was given. It keeps the
// shortest and the longest rather than the latest, because the limiter's sweep
// goroutine reads settings through the same instance and its read is meant to be
// the longer one: an assertion on the most recent read would depend on which of
// them landed last.
type deadlineSpy struct {
	SettingsReader
	mu       sync.Mutex
	seen     bool
	shortest time.Duration
	longest  time.Duration
}

func (d *deadlineSpy) GetDuration(ctx context.Context, key string, def time.Duration) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		budget := time.Until(deadline)
		d.mu.Lock()
		if !d.seen || budget < d.shortest {
			d.shortest = budget
		}
		if !d.seen || budget > d.longest {
			d.longest = budget
		}
		d.seen = true
		d.mu.Unlock()
	}
	return d.SettingsReader.GetDuration(ctx, key, def)
}

// budgets reports the shortest and longest budget any read was given, and
// whether any read carried a deadline at all.
func (d *deadlineSpy) budgets() (shortest, longest time.Duration, seen bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shortest, d.longest, d.seen
}

// TestTPMLimiter_OwnerAdmissionClaimsTheHorizon covers the other admission
// surface: the owner's aggregate bucket is resolved through its own call site,
// with a context assembled from the session rather than a virtual key, and its
// memo has to claim a horizon the same way.
func TestTPMLimiter_OwnerAdmissionClaimsTheHorizon(t *testing.T) {
	l, s := newTestTPMLimiter(t)
	s.set(settingsKeyRequestTimeout, "1h")
	h := l.UserMiddleware(true)(okHandler())

	userTPM := 600
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sessionTPMReq("uid-1", &userTPM))
	if rec.Code != http.StatusOK {
		t.Fatalf("the first session request should pass, got %d", rec.Code)
	}

	if got := remainingMemoHorizon(t, l, userBucketKey("uid-1")); got != 40*time.Hour {
		t.Errorf("an owner admission should claim the horizon the setting implies, got %v", got)
	}
}

// ageMemo winds a cap memo's deadline back by d, standing in for d of elapsed
// time without sleeping. The memo's claimed horizon is unchanged; only how much
// of it is left moves.
func ageMemo(t *testing.T, l *TPMLimiter, key string, d time.Duration) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	memo, ok := l.caps[key]
	if !ok {
		t.Fatalf("no cap memo for %q to age", key)
	}
	memo.expiresAt = memo.expiresAt.Add(-d)
}

// remainingMemoHorizon reports the horizon a memo claimed at admission, rounded to the
// minute so the microseconds between stamping it and reading it do not matter.
func remainingMemoHorizon(t *testing.T, l *TPMLimiter, key string) time.Duration {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	memo, ok := l.caps[key]
	if !ok {
		t.Fatalf("no cap memo for %q to measure", key)
	}
	return time.Until(memo.expiresAt).Round(time.Minute)
}

// memoLives reports whether a cap memo is still in the map.
func memoLives(l *TPMLimiter, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.caps[key]
	return ok
}
