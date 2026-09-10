package ratelimit

import (
	"context"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
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
	// caps remembers the budget each bucket was last sized to, so a debit that
	// arrives after the idle sweep evicted the bucket can rebuild it and land
	// the charge instead of dropping it. A request streaming for longer than
	// the idle cutoff, on a key with no other traffic, is exactly that case.
	//
	// Entries outlive the buckets and are swept on their own, much longer,
	// horizon, so the map does not grow by one entry for every key the process
	// ever admitted.
	caps     map[string]*capMemo
	settings SettingsReader
	stopCh   chan struct{}
	// lastSlowReadWarn is when the slow-horizon-read warning was last emitted,
	// in Unix nanoseconds, so a hanging database costs one line per interval
	// rather than one per admission.
	lastSlowReadWarn atomic.Int64
	// lastGoodHorizon is the longest horizon any completed settings read has
	// produced, so a read that times out can fall back to what this gateway was
	// actually running rather than to the default's floor. It is a high-water
	// mark rather than the latest value because a read that fails without
	// timing out also returns the default, and letting that overwrite the mark
	// would erase the long horizon exactly when it is needed. Only the fallback
	// consults it, so holding it high costs retention and never a debit.
	lastGoodHorizon atomic.Int64
}

// slowReadWarnInterval is the shortest gap between two slow-horizon-read
// warnings. Long enough that a hanging database cannot crowd the log, short
// enough that the condition stays visible while it lasts.
const slowReadWarnInterval = time.Minute

// capMemo is the budget a bucket was last built with, kept so an evicted bucket
// can be rebuilt to take a late debit.
type capMemo struct {
	tpm int
	// expiresAt is when this memo may be swept: the latest horizon any admission
	// against it has claimed. The claim is made when the bucket is resolved,
	// before the budget decides, so a request turned away with a 429 has claimed
	// one too, and it only ever pushes the deadline out.
	//
	// That is what makes changing request_timeout safe in both directions.
	// Lowering it cannot pull a deadline back in. Raising it cannot outrun one
	// either, because the proxy fixes a request's timeout once, before its first
	// attempt, so a request already running keeps the timeout its claim was
	// sized against.
	//
	// The claims are absolute times rather than a duration, so a long timeout
	// inflates the deadline only until the request it was claimed for could have
	// finished, after which ordinary admissions carry the memo again. A
	// request_timeout large enough to saturate the horizon is the exception: it
	// pushes the deadline centuries out and holds the memo for the life of the
	// process. Either way the map is keyed by virtual key hash and owner id, so
	// it is bounded by the rows those come from rather than by traffic.
	expiresAt time.Time
}

// settingsKeyRequestTimeout is the per-attempt upstream timeout the proxy reads
// for every request, through the same GetDuration and the same one minute
// default repeated below. The limiter reads it only to size the cap-memo horizon
// against the longest request the gateway can hold open, and follows the proxy's
// reading of it rather than setting one of its own. The two are held in lockstep
// by hand: moving the proxy's key or default means moving these with it, or the
// horizon is sized against a timeout the gateway no longer uses.
const settingsKeyRequestTimeout = "request_timeout"

// defaultRequestTimeout mirrors the proxy's fallback for an unset
// request_timeout, so an unconfigured gateway derives the same horizon the
// proxy's own default implies.
const defaultRequestTimeout = time.Minute

// settingsReadTimeout bounds the horizon lookup on the admission path. The value
// only sizes a retention window measured in days, so it is not worth waiting on:
// this is generous for one indexed row that the settings cache usually answers
// outright, and short enough that a database which has stopped answering costs
// an admission a barely visible pause and whatever horizon is already known.
const settingsReadTimeout = 100 * time.Millisecond

// markRefreshTimeout bounds the same lookup off the request path, where a slow
// database can be waited out because nobody is being held up for it.
const markRefreshTimeout = 5 * time.Second

// minCapMemoTTL floors the cap-memo horizon. Against the factor below, the
// crossover is a 36 minute request_timeout: anything shorter derives less than a
// day and lands on this floor, which is every ordinary configuration, so the
// floor is what the fleet actually runs on and the derived horizon only takes
// over above that.
const minCapMemoTTL = 24 * time.Hour

// capMemoTimeoutFactor scales request_timeout into that horizon. A streaming or
// long-running request gets ten times request_timeout per attempt, and the
// proxy's overall deadline, itself derived from that same per-attempt budget
// rather than configured separately, caps a whole request at twice it. So twenty
// times the setting is the longest a request can live, and doubling that again
// leaves the memo a full request's worth of margin over the sweep.
const capMemoTimeoutFactor = 40

// capMemoTTL is how long a cap memo outlives its bucket. The memo is what lets a
// late debit rebuild an evicted bucket, so it has to outlast the longest request
// the gateway can hold open, and that ceiling is not a constant: it is derived
// from request_timeout, which an operator sets with no upper bound. Deriving the
// horizon from the same setting means raising the timeout cannot silently
// reopen the dropped-debit hole the memo exists to close.
//
// A request_timeout that is unset or unparseable reads as the proxy's own
// default and derives the floor. So does an explicit zero or negative one,
// which is not "no timeout" on the proxy's side either: it hands the upstream
// attempt a context that has already expired. One large enough to overflow the
// multiplication saturates rather than wrapping, since a wrapped product
// collapses onto the floor, far shorter than the request it has to outlast, and
// the memo would be swept out from under it. Saturation does not scale with such
// a timeout, it only stops the arithmetic running backwards, and what it costs
// is holding those memos for the life of the process.
func capMemoTTL(requestTimeout time.Duration) time.Duration {
	if requestTimeout <= 0 {
		return minCapMemoTTL
	}
	if requestTimeout > math.MaxInt64/capMemoTimeoutFactor {
		return time.Duration(math.MaxInt64)
	}
	return max(minCapMemoTTL, capMemoTimeoutFactor*requestTimeout)
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
		caps:     make(map[string]*capMemo),
		settings: settings,
		stopCh:   make(chan struct{}),
	}
	go runCleanup(l.stopCh, l.sweep)
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
			// "user:<uuid>" budget. Completion passes the same owner back to
			// Debit, so the charge cannot land on a different owner than the
			// admission did, however the key is reassigned meanwhile.
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

			entry := l.getEntry(r.Context(), keyHash, tpm)
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
	userEntry := l.getEntry(ctx, userKey, userTPM)
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
// are throttled, and charges the same total to the owner's aggregate bucket
// when the request carried an owner.
//
// The owner comes from the request that is completing, not from a lookup: a key
// reassigned to another user mid-request would otherwise charge whoever owns it
// at completion rather than whoever was admitted. It is a no-op for a bucket
// that neither exists nor has a live cap memo to rebuild from, which is what no
// cap in effect looks like. Safe for concurrent use.
func (l *TPMLimiter) Debit(keyHash, ownerUserID string, tokens int) {
	if tokens <= 0 {
		return
	}
	l.debitBucket(keyHash, tokens)
	if ownerUserID != "" {
		l.debitBucket(userBucketKey(ownerUserID), tokens)
	}
}

// DebitUser removes the actual token total from an owner's aggregate bucket
// directly, for requests that never had a virtual key to debit through (the
// admin chat surface). Debit already reaches the owner bucket for keyed
// requests, so the two are mutually exclusive: calling both for one request
// would charge the owner twice.
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
// idle cutoff spend a key's whole minute for free. A bucket with neither an
// entry nor a live cap memo has no cap in effect, and is a no-op. Safe for
// concurrent use.
func (l *TPMLimiter) debitBucket(bucketKey string, tokens int) {
	l.mu.Lock()
	entry, ok := l.buckets[bucketKey]
	switch {
	case ok:
		entry.lastUsed = time.Now()
	default:
		if memo, known := l.caps[bucketKey]; known && memo.tpm > 0 {
			// The memo's own deadline is left alone: it was claimed by the
			// request this debit is closing, and the next admission on this key
			// claims its own.
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
//
// It also claims the key's cap-memo horizon, which is why it takes a context:
// resolving that horizon reads request_timeout, and it does so before taking
// the lock every admission blocks on.
func (l *TPMLimiter) getEntry(ctx context.Context, keyHash string, tpm int) *tpmEntry {
	// Resolved before taking the lock, since every admission blocks on this mutex.
	// The deadline itself is stamped under the lock, so waiting for it does not
	// eat into the horizon the request just claimed.
	horizon := l.memoHorizon(ctx)

	l.mu.Lock()
	defer l.mu.Unlock()

	memoExpiry := time.Now().Add(horizon)

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
	if memo, known := l.caps[keyHash]; known {
		memo.tpm = tpm
		if memoExpiry.After(memo.expiresAt) {
			memo.expiresAt = memoExpiry
		}
	} else {
		l.caps[keyHash] = &capMemo{tpm: tpm, expiresAt: memoExpiry}
	}
	return entry
}

// memoHorizon returns the cap-memo horizon request_timeout currently implies.
// The settings layer serves it from its own cache, and admission already reads
// that layer before reaching here, so this is not a new dependency on it. It is
// deliberately not cached again: a second cache would double how long a raised
// request_timeout goes unnoticed, and during that window a memo can be stamped
// shorter than the request it has to outlast, which is the hole this closes.
//
// The read is detached from the caller's cancellation because the horizon is a
// property of the setting, not of the request that happened to trigger the
// refresh: a client that disconnects during admission does not stop the proxy
// completing the upstream call and debiting it, and a cancelled read would
// claim the floor for a request entitled to much longer. Detaching also drops
// the caller's deadline, and the settings repository takes its query deadline
// from the caller and sets none of its own, so the read carries a bound here
// instead, sized so that waiting one out costs an admission far less than the
// value being read is worth.
//
// Exceeding that bound is the one failure this can recognise, and there it falls
// back to the longest horizon a completed read has produced rather than to the
// default's floor, which on a gateway running a long request_timeout would be
// shorter than the requests being admitted. That fallback cannot pin a lowered
// timeout, because it is consulted only when the read timed out: a read that
// completes always returns what the setting currently says. While the database
// stays slow enough that none of them complete, admissions do keep claiming the
// remembered horizon, which lengthens retention and nothing else.
//
// A read that fails outright is a different matter: GetDuration cannot tell one
// from an unset key, so it reads as the default and the horizon does drop to the
// floor. A request admitted in that window and still running a day later loses
// its debit.
func (l *TPMLimiter) memoHorizon(ctx context.Context) time.Duration {
	readCtx, cancelRead := context.WithTimeout(context.WithoutCancel(ctx), settingsReadTimeout)
	defer cancelRead()

	horizon := capMemoTTL(l.settings.GetDuration(readCtx, settingsKeyRequestTimeout, defaultRequestTimeout))
	// Recorded before the deadline is examined, so a read that answered in the
	// same instant it expired still counts. The mark only rises, so recording a
	// genuine timeout's default costs nothing.
	l.rememberHorizon(horizon)
	// Only a value the default itself derives can have come from a read that
	// gave up: anything else was answered from the setting, whatever the
	// deadline did in the moment after. That distinction matters because the
	// deadline can fire between the read returning and this check, and a lowered
	// request_timeout must not be discarded on the strength of that. A setting
	// that genuinely derives the floor is the one case still caught by it, and
	// there the fallback only lengthens retention.
	if readCtx.Err() == nil || horizon != capMemoTTL(defaultRequestTimeout) {
		return horizon
	}

	// The read ran out of time, so what came back is the default's horizon
	// rather than this gateway's. Keep whichever is longer: too long only costs
	// retention, too short costs a debit.
	mark := time.Duration(l.lastGoodHorizon.Load())
	remembered := mark > horizon
	if remembered {
		horizon = mark
	}
	if l.warnedSlowRead() {
		// Worth a line, since the horizon is no longer coming from the setting.
		// Rate limited: a database that hangs does it to every admission at once.
		debuglog.Warn("ratelimit: timed out reading request_timeout, cap memos fall back",
			"horizon", horizon, "remembered", remembered)
	}
	return horizon
}

// rememberHorizon raises the high-water mark a timed-out read falls back to.
// Racing readers retry rather than clobber, so the mark only ever climbs.
func (l *TPMLimiter) rememberHorizon(horizon time.Duration) {
	for {
		mark := l.lastGoodHorizon.Load()
		if time.Duration(mark) >= horizon {
			return
		}
		if l.lastGoodHorizon.CompareAndSwap(mark, int64(horizon)) {
			return
		}
	}
}

// warnedSlowRead reports whether this slow horizon read is the one that gets to
// speak, claiming the interval if so. Two racing readers can both see a stale
// timestamp, and the loser's compare-and-swap fails, so at most one line is
// emitted per interval.
func (l *TPMLimiter) warnedSlowRead() bool {
	now := time.Now()
	last := l.lastSlowReadWarn.Load()
	// A negative gap means the wall clock stepped backwards over the stamp.
	// Treating that as "not yet due" would silence the warning until the clock
	// caught up, so only a gap that is both forwards and short suppresses it.
	if gap := now.Sub(time.Unix(0, last)); last != 0 && gap >= 0 && gap < slowReadWarnInterval {
		return false
	}
	return l.lastSlowReadWarn.CompareAndSwap(last, now.UnixNano())
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

// sweep is what the background loop runs: it refreshes the horizon the admission
// path falls back to, then evicts what has aged out. Kept separate from cleanup
// so tests can drive the eviction without a settings read.
func (l *TPMLimiter) sweep() {
	l.refreshHorizonMark()
	l.cleanup()
}

// refreshHorizonMark reads request_timeout with a bound an admission could not
// afford and records what it implies. Without it a settings store that is
// consistently slower than settingsReadTimeout would never let a read complete,
// so the mark could never rise above the default's floor and a raised
// request_timeout would go unnoticed for the life of the process.
func (l *TPMLimiter) refreshHorizonMark() {
	ctx, cancel := context.WithTimeout(context.Background(), markRefreshTimeout)
	defer cancel()

	l.rememberHorizon(capMemoTTL(l.settings.GetDuration(ctx, settingsKeyRequestTimeout, defaultRequestTimeout)))
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
	// Cap memos are what let a debit rebuild an evicted bucket, so they are held
	// far longer than the bucket itself. Each carries the deadline its own
	// admissions claimed, so an operator changing request_timeout cannot strand a
	// request that was already admitted. Past that deadline no request the memo
	// was written for can still be running, and keeping it would grow the map by
	// one entry for every key the process ever saw.
	for key, memo := range l.caps {
		if now.After(memo.expiresAt) {
			delete(l.caps, key)
		}
	}
}
