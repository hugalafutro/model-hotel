package ratelimit

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
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
// admit-on-past-consumption / debit-on-completion: admission reserves one
// placeholder token (Allow / ReserveN, which also closes the concurrent
// admission race), and the actual token total is debited afterwards (Debit) on
// top of that reservation. Each admitted request thus costs its real total plus
// one placeholder token; the reservation is not reconciled against the debit,
// so it is a small restrictive surcharge, negligible at realistic budgets. A
// key can still overshoot by up to its in-flight requests' worth of tokens
// (bounded in practice by the separate RPS limiter's burst), the standard
// behaviour for token rate limiting.
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
	assoc map[string]*assocEntry
	// caps remembers the budget each bucket was last sized to, so a debit that
	// arrives after the idle sweep evicted the bucket can rebuild it and land
	// the charge instead of dropping it. A request streaming for longer than
	// the idle cutoff, on a key with no other traffic, is exactly that case,
	// and it takes the key's assoc entry with it, so the owner's aggregate
	// needs the same memory or the owner still gets its minute free.
	//
	// Entries outlive the buckets and are swept on their own, much longer,
	// horizon: past capMemoTTL no request can still be in flight, so keeping
	// them would only grow the map for every key the process ever admitted.
	caps     map[string]*capMemo
	settings SettingsReader
	stopCh   chan struct{}
}

// capMemo is the budget (and owner bucket, for a key) a bucket was last built
// with, kept so an evicted bucket can be rebuilt to take a late debit.
type capMemo struct {
	tpm      int
	userKey  string
	lastUsed time.Time
}

// capMemoTTL bounds how long a cap memo outlives its bucket. It has to exceed
// the longest request the gateway will hold open, which the stall watchdog and
// the upstream timeouts keep far below an hour.
const capMemoTTL = time.Hour

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
		caps:     make(map[string]*capMemo),
		settings: settings,
		stopCh:   make(chan struct{}),
	}
	go runCleanup(l.stopCh, l.cleanup)
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
			userKey, _ := userTPMFromCtx(r.Context())
			l.setAssoc(keyHash, userKey)
			// The owner token is reserved (not committed) here and only kept
			// once every later gate passes. A request that clears this stage
			// but is then rejected by the per-key gate cancels the reservation,
			// so a rejected request never burns an owner token (Debit only runs
			// for admitted requests). Without this a single over-cap key would
			// drain the shared owner budget and 429 the owner's other keys.
			//
			// The reservation and its cancellation are pinned to a single `now`
			// on purpose: an immediately actionable reservation (Delay 0) has
			// timeToAct == now, and CancelAt only restores tokens when
			// timeToAct is not before the cancel time. Using time.Now() again
			// at cancel would make timeToAct earlier than it, turning Cancel
			// into a no-op and defeating the whole point.
			now := time.Now()
			userRes, ok := l.admitUserTPM(r.Context(), w, now)
			if !ok {
				return
			}

			tpm := l.effectiveTPM(r.Context())
			if tpm <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			entry := l.getEntry(keyHash, tpm)
			// Allow() atomically reserves one admission token under the
			// limiter's mutex; concurrent requests cannot all pass the same
			// non-mutating peek. The reserved token is a placeholder that is
			// not reconciled: Debit later charges the full total on top of it,
			// a ~1-token restrictive surcharge, negligible at realistic budgets.
			if !entry.limiter.Allow() {
				// This request cleared the owner stage but fails here, so return
				// its held owner token. Leaving it committed would let sustained
				// per-key rejections drain the shared owner budget even though
				// none of those requests ever run (Debit is never called).
				if userRes != nil {
					userRes.CancelAt(now)
				}
				httpx.SetRetryAfter(w, time.Duration(tpmRetryAfter(entry.limiter))*time.Second)
				util.WriteOpenAIError(w, "token rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserMiddleware enforces ONLY the owner-level aggregate TPM budget, for
// surfaces that authenticate a dashboard session rather than a virtual key
// (/api/chat/*, mounted by proxy.RegisterAdminChat). It shares the same two
// kill-switches as Middleware.
//
// The full Middleware is deliberately NOT reused there. Its per-key stage keys
// on extractKey, which falls back to the resolved client address when no virtual key is in
// context, and that bucket has no debit path: the completion half (Debit) is
// driven by the virtual-key hash, which a session-authenticated request does
// not have. Mounting the full middleware would therefore create an address-keyed
// bucket that is admitted against but never charged, i.e. a cap that looks
// enforced and is not. The owner bucket is charged, via DebitUser.
//
// A caller with no owner-level TPM cap passes straight through: user-level caps
// have no global-settings fallback (see ctxkeys.UserRateLimitTPMKey).
func (l *TPMLimiter) UserMiddleware(enabled bool) func(http.Handler) http.Handler {
	if !enabled {
		debuglog.Info("ratelimit: per-user TPM limiting disabled via env (RATE_LIMIT_ENABLED=false)")
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
			// The reservation is intentionally discarded: this is the last
			// MIDDLEWARE on the group, so nothing downstream of here can hand
			// the token back. The handler still rejects some requests after
			// this point — 403 when the caller's provider cap excludes every
			// candidate (resolveCandidates), 404 for an unknown model, 400 for
			// an unparseable body — and each of those keeps the 1-token
			// placeholder. That is the same behaviour as /v1, where only the
			// per-key middleware stage ever cancels an owner reservation, and
			// it is symmetric on purpose.
			if _, ok := l.admitUserTPM(r.Context(), w, time.Now()); !ok {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// admitUserTPM runs the owner-level aggregate stage of TPM admission: it takes
// one placeholder token from the caller's "user:<uuid>" bucket, sized to this
// member's fleet fair share of the owner's cap.
//
// It returns (nil, true) when there is nothing to enforce (unowned request, or
// an owner with no TPM cap), (reservation, true) on admission, and (nil, false)
// when it has already written the 429 response — in which case the caller must
// return without serving. The reservation is handed back so a caller with a
// further gate can CancelAt(now) it and hand the token back; `now` must be the
// same instant passed in here, or CancelAt silently does nothing.
func (l *TPMLimiter) admitUserTPM(ctx context.Context, w http.ResponseWriter, now time.Time) (*rate.Reservation, bool) {
	userKey, userTPM := userTPMFromCtx(ctx)
	if userKey == "" {
		return nil, true
	}
	userTPM = fleetShareTPM(ctx, l.settings, userTPM)
	userEntry := l.getEntry(userKey, userTPM)
	// ReserveN takes one admission token under the limiter's mutex, so
	// concurrent requests can't all pass the same non-mutating peek and blow
	// the budget. Unlike Allow() the reservation is cancellable;
	// !OK() || DelayFrom(now) > 0 is the exact equivalent of Allow() returning
	// false (no token available right now). The reserved token is a placeholder
	// that is not reconciled: the debit later charges the full total on top of
	// it, a ~1-token restrictive surcharge, negligible at realistic budgets.
	userRes := userEntry.limiter.ReserveN(now, 1)
	if !userRes.OK() || userRes.DelayFrom(now) > 0 {
		userRes.CancelAt(now)
		httpx.SetRetryAfter(w, time.Duration(tpmRetryAfter(userEntry.limiter))*time.Second)
		util.WriteOpenAIError(w, "user token rate limit exceeded", http.StatusTooManyRequests)
		return nil, false
	}
	return userRes, true
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
	// one (recorded at admission). The idle sweep takes the assoc entry at the
	// same cutoff as the bucket, so a request that outlives it falls back to the
	// cap memo: without that the key's own budget is charged and the owner's
	// aggregate is not, which is the same free minute one layer up.
	l.mu.Lock()
	userKey := ""
	if a, ok := l.assoc[keyHash]; ok {
		a.lastUsed = time.Now()
		userKey = a.userKey
	} else if memo, ok := l.caps[keyHash]; ok {
		memo.lastUsed = time.Now()
		userKey = memo.userKey
	}
	l.mu.Unlock()
	if userKey != "" {
		l.debitBucket(userKey, tokens)
	}
}

// DebitUser removes the actual token total from an owner's aggregate bucket
// directly, for requests that never had a virtual key to debit through (the
// admin chat surface). Debit already reaches the owner bucket for keyed
// requests via the association recorded at admission, so the two are mutually
// exclusive: calling both for one request would charge the owner twice.
//
// No-op when the owner has no bucket, which is what "no owner-level TPM cap"
// looks like — admission creates the bucket, so a capped request always has one
// by completion.
func (l *TPMLimiter) DebitUser(userID string, tokens int) {
	if userID == "" || tokens <= 0 {
		return
	}
	l.debitBucket(userBucketKey(userID), tokens)
}

// debitBucket removes tokens from one bucket. A bucket the idle sweep evicted
// while the request was still in flight is rebuilt at its remembered budget so
// the charge still lands: dropping it would let a request that outlives the
// idle cutoff spend a key's whole minute for free. A key with no remembered
// budget has no cap in effect, and is a no-op. Safe for concurrent use.
func (l *TPMLimiter) debitBucket(bucketKey string, tokens int) {
	l.mu.Lock()
	entry, ok := l.buckets[bucketKey]
	switch {
	case ok:
		entry.lastUsed = time.Now()
	default:
		if memo, known := l.caps[bucketKey]; known && memo.tpm > 0 {
			memo.lastUsed = time.Now()
			entry = &tpmEntry{
				limiter:  rate.NewLimiter(rate.Limit(float64(memo.tpm)/60.0), memo.tpm),
				tpm:      memo.tpm,
				lastUsed: time.Now(),
			}
			l.buckets[bucketKey] = entry
			ok = true
		}
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
		n := min(remaining, burst)
		entry.limiter.ReserveN(now, n)
		remaining -= n
	}
}

// setAssoc records (or clears, when userKey is empty) the key-to-owner bucket
// association consulted by Debit. The owner is mirrored into the key's cap memo,
// which outlives the assoc entry, so a debit arriving after the sweep still
// finds the owner bucket to charge.
func (l *TPMLimiter) setAssoc(keyHash, userKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	memo, ok := l.caps[keyHash]
	if !ok {
		memo = &capMemo{}
		l.caps[keyHash] = memo
	}
	memo.userKey = userKey
	memo.lastUsed = time.Now()
	if userKey == "" {
		delete(l.assoc, keyHash)
		return
	}
	l.assoc[keyHash] = &assocEntry{userKey: userKey, lastUsed: time.Now()}
}

// userBucketKey namespaces an owner's aggregate bucket away from the key-hash
// buckets sharing TPMLimiter's map. Every TPMLimiter writer and reader of that
// bucket must go through it, or an admission and its debit land on two
// different buckets.
//
// Scoped to TPMLimiter deliberately: Limiter (limiter.go) builds the same
// "user:"+uid string inline for its own RPS map, which this helper does not
// reach and does not need to. The two maps are independent, so a divergence
// could not cross-fault a bucket, but the spellings must stay identical.
func userBucketKey(userID string) string {
	return "user:" + userID
}

// userTPMFromCtx resolves the owner's aggregate bucket key and TPM cap from
// the request context. Returns "" when the request is unowned or the owner has
// no TPM cap (there is no global fallback for user-level caps).
//
// Both halves are published by ProxyKeyMiddleware from the virtual key's owner
// on /v1, and by api.ChatUserContextMiddleware from the session's own account
// on /api/chat/*.
func userTPMFromCtx(ctx context.Context) (string, int) {
	uid, ok := ctx.Value(ctxkeys.VirtualKeyOwnerIDKey).(string)
	if !ok || uid == "" {
		return "", 0
	}
	tpm, ok := ctx.Value(ctxkeys.UserRateLimitTPMKey).(*int)
	if !ok || tpm == nil || *tpm <= 0 {
		return "", 0
	}
	return userBucketKey(uid), *tpm
}

// effectiveTPM resolves the per-minute cap for the current request: the per-key
// override from context if set, otherwise the global default from settings. The
// resolved cap is split into this member's fleet fair share before use.
func (l *TPMLimiter) effectiveTPM(ctx context.Context) int {
	if v := ctx.Value(ctxkeys.VirtualKeyRateLimitTPMKey); v != nil {
		if p, ok := v.(*int); ok && p != nil {
			return fleetShareTPM(ctx, l.settings, *p)
		}
	}
	return fleetShareTPM(ctx, l.settings, l.settings.GetInt(ctx, settingsKeyTPM, defaultTPM))
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
	memo, ok := l.caps[keyHash]
	if !ok {
		memo = &capMemo{}
		l.caps[keyHash] = memo
	}
	memo.tpm = tpm
	memo.lastUsed = time.Now()
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
	secs := max(int(math.Ceil((1-avail)/perSec)), 1)
	return secs
}

func (l *TPMLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-10 * time.Minute)
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
	// Cap memos are what let a debit rebuild an evicted bucket, so they are held
	// far longer than the bucket itself. Past this horizon no request that was
	// admitted against the memo can still be running, and keeping it would grow
	// the map by one entry for every key the process ever saw.
	memoCutoff := now.Add(-capMemoTTL)
	for key, memo := range l.caps {
		if memo.lastUsed.Before(memoCutoff) {
			delete(l.caps, key)
		}
	}
}
