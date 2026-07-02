package ratelimit

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// settingsKeyTPM is the optional global default tokens-per-minute cap. 0 (the
// default) means "no global cap"; per-key rate_limit_tpm overrides it.
const settingsKeyTPM = "rate_limit_tpm"

// defaultTPM is the fallback when no DB setting is present: no global cap.
const defaultTPM = 0

// TPMLimiter enforces a per-virtual-key tokens-per-minute budget, separate
// from the requests/sec Limiter. It is a consumer-side control: when a key's
// minute token budget is drained, its next request is rejected with 429. The
// upstream provider is never throttled.
//
// Because a request's token cost is unknown until it completes, enforcement is
// admit-on-past-consumption / debit-on-completion: admission checks the current
// budget (Allow), and the actual token total is debited afterwards (Debit). A
// key can therefore overshoot by ~one in-flight request's worth of tokens; this
// is the standard, accepted behaviour for token rate limiting.
//
// Like Limiter, the budget lives in-process and is NOT consistent across
// replicas behind a load balancer (effective limit is ~N× configured with N
// replicas). This is the same limitation the RPS limiter has today.
type TPMLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tpmEntry
	// assoc maps a key hash to its owner's "user:<uuid>" bucket key so Debit
	// (which only knows the key hash) can also debit the owner's aggregate
	// bucket. Refreshed on every admission, evicted alongside idle buckets.
	assoc    map[string]*assocEntry
	settings SettingsReader
	stopCh   chan struct{}
}

type assocEntry struct {
	userKey  string
	lastUsed time.Time
}

// tpmEntry is a per-key token-budget bucket. The rate.Limiter is configured as
// limit = tpm/60 tokens refilled per second, burst = tpm (a full minute's
// budget available at once), giving a smooth sliding budget.
type tpmEntry struct {
	limiter  *rate.Limiter
	tpm      int
	lastUsed time.Time
}

// NewTPMLimiter creates a TPMLimiter reading configuration from the provided
// SettingsReader. A background goroutine evicts buckets idle for >10 minutes.
func NewTPMLimiter(settings SettingsReader) *TPMLimiter {
	l := &TPMLimiter{
		buckets:  make(map[string]*tpmEntry),
		assoc:    make(map[string]*assocEntry),
		settings: settings,
		stopCh:   make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Stop terminates the background cleanup goroutine. Call during shutdown.
func (l *TPMLimiter) Stop() {
	close(l.stopCh)
}

// Middleware returns an HTTP middleware enforcing the per-key TPM budget at
// admission. It shares the rate-limiter kill-switches: the enabled parameter
// (env RATE_LIMIT_ENABLED) and the DB "rate_limit_enabled" runtime toggle.
//
// On budget exhaustion it responds with 429 and a Retry-After header. When the
// effective TPM is <= 0 (no per-key cap and no global default) it is a no-op.
func (l *TPMLimiter) Middleware(enabled bool) func(http.Handler) http.Handler {
	if !enabled {
		debuglog.Info("ratelimit: per-key TPM limiting disabled via env (RATE_LIMIT_ENABLED=false)")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}
			if !l.settings.GetBool(r.Context(), settingsKeyEnabled, true) {
				next.ServeHTTP(w, r)
				return
			}

			keyHash := extractKey(r)
			if keyHash == "" {
				next.ServeHTTP(w, r)
				return
			}

			// User-level aggregate stage: all keys owned by one user share a
			// "user:<uuid>" budget. The key-to-owner association is recorded
			// (or cleared) on every admission so Debit, which only sees the
			// key hash, can debit the owner's bucket too.
			userKey, userTPM := userTPMFromCtx(r.Context())
			l.setAssoc(keyHash, userKey)
			if userKey != "" {
				userEntry := l.getEntry(userKey, userTPM)
				if userEntry.limiter.Tokens() < 1 {
					w.Header().Set("Retry-After", strconv.Itoa(tpmRetryAfter(userEntry.limiter)))
					util.WriteOpenAIError(w, "user token rate limit exceeded", http.StatusTooManyRequests)
					return
				}
			}

			tpm := l.effectiveTPM(r.Context())
			if tpm <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			entry := l.getEntry(keyHash, tpm)
			if entry.limiter.Tokens() < 1 {
				w.Header().Set("Retry-After", strconv.Itoa(tpmRetryAfter(entry.limiter)))
				util.WriteOpenAIError(w, "token rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Allow reports whether a request may be admitted for keyHash under the given
// per-minute token budget. tpm <= 0 means no cap (always allowed). Exposed for
// testing and reuse; the Middleware performs the same check inline.
func (l *TPMLimiter) Allow(keyHash string, tpm int) bool {
	if tpm <= 0 {
		return true
	}
	return l.getEntry(keyHash, tpm).limiter.Tokens() >= 1
}

// Debit removes the actual token total from a key's budget after a request
// completes, driving the budget toward (and past) zero so subsequent requests
// are throttled. It is a no-op when no bucket exists for the key (no cap in
// effect, or the bucket was evicted) — admission creates the bucket, so a
// capped request always has one by completion. Safe for concurrent use.
func (l *TPMLimiter) Debit(keyHash string, tokens int) {
	if tokens <= 0 {
		return
	}
	l.debitBucket(keyHash, tokens)
	// Also debit the owner's aggregate bucket when the key is associated with
	// one (recorded at admission).
	l.mu.Lock()
	a, ok := l.assoc[keyHash]
	if ok {
		a.lastUsed = time.Now()
	}
	l.mu.Unlock()
	if ok && a.userKey != "" {
		l.debitBucket(a.userKey, tokens)
	}
}

// debitBucket removes tokens from one bucket. No-op when the bucket does not
// exist (no cap in effect, or evicted) — admission creates the bucket, so a
// capped request always has one by completion. Safe for concurrent use.
func (l *TPMLimiter) debitBucket(bucketKey string, tokens int) {
	l.mu.Lock()
	entry, ok := l.buckets[bucketKey]
	if ok {
		entry.lastUsed = time.Now()
	}
	l.mu.Unlock()
	if !ok {
		return
	}

	// ReserveN fails (and debits nothing) when N exceeds the limiter's burst.
	// A single response can legitimately exceed a minute's budget, so debit in
	// burst-sized chunks; each chunk succeeds and accumulates "debt" (negative
	// tokens) that refills at tpm/60 per second. This is what makes an
	// over-budget request block the next one until the window recovers.
	remaining := tokens
	burst := entry.limiter.Burst()
	if burst <= 0 {
		return
	}
	now := time.Now()
	for remaining > 0 {
		n := remaining
		if n > burst {
			n = burst
		}
		entry.limiter.ReserveN(now, n)
		remaining -= n
	}
}

// setAssoc records (or clears, when userKey is empty) the key-to-owner bucket
// association consulted by Debit.
func (l *TPMLimiter) setAssoc(keyHash, userKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if userKey == "" {
		delete(l.assoc, keyHash)
		return
	}
	l.assoc[keyHash] = &assocEntry{userKey: userKey, lastUsed: time.Now()}
}

// userTPMFromCtx resolves the owner's aggregate bucket key and TPM cap from
// the request context. Returns "" when the key is unowned or the owner has no
// TPM cap (there is no global fallback for user-level caps).
func userTPMFromCtx(ctx context.Context) (string, int) {
	uid, ok := ctx.Value(ctxkeys.VirtualKeyOwnerIDKey).(string)
	if !ok || uid == "" {
		return "", 0
	}
	tpm, ok := ctx.Value(ctxkeys.UserRateLimitTPMKey).(*int)
	if !ok || tpm == nil || *tpm <= 0 {
		return "", 0
	}
	return "user:" + uid, *tpm
}

// effectiveTPM resolves the per-minute cap for the current request: the per-key
// override from context if set, otherwise the global default from settings.
func (l *TPMLimiter) effectiveTPM(ctx context.Context) int {
	if v := ctx.Value(ctxkeys.VirtualKeyRateLimitTPMKey); v != nil {
		if p, ok := v.(*int); ok && p != nil {
			return *p
		}
	}
	return l.settings.GetInt(ctx, settingsKeyTPM, defaultTPM)
}

// getEntry returns (or creates) the token-budget bucket for keyHash. If the
// stored bucket's tpm no longer matches (the key's cap changed at runtime) it
// is replaced so the new budget takes effect immediately.
func (l *TPMLimiter) getEntry(keyHash string, tpm int) *tpmEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.buckets[keyHash]
	if !ok || entry.tpm != tpm {
		entry = &tpmEntry{
			limiter:  rate.NewLimiter(rate.Limit(float64(tpm)/60.0), tpm),
			tpm:      tpm,
			lastUsed: time.Now(),
		}
		l.buckets[keyHash] = entry
	} else {
		entry.lastUsed = time.Now()
	}
	return entry
}

// tpmRetryAfter estimates seconds until at least one token is available again,
// for the Retry-After header. Always >= 1.
func tpmRetryAfter(lim *rate.Limiter) int {
	avail := lim.Tokens()
	if avail >= 1 {
		return 1
	}
	perSec := float64(lim.Limit())
	if perSec <= 0 {
		return 1
	}
	secs := int(math.Ceil((1 - avail) / perSec))
	if secs < 1 {
		secs = 1
	}
	return secs
}

// cleanupLoop periodically evicts idle buckets to bound memory.
func (l *TPMLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

func (l *TPMLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for key, entry := range l.buckets {
		if entry.lastUsed.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
	for key, a := range l.assoc {
		if a.lastUsed.Before(cutoff) {
			delete(l.assoc, key)
		}
	}
}
