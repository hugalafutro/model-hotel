package budget

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/httpx"
	"github.com/hugalafutro/model-hotel/internal/util"
)

const logComponent = "budget"

// Subject is what a budget binds to: one virtual key or one user. The auth
// middleware publishes one per bound budget on the request context.
type Subject struct {
	Kind   string // KindKey or KindUser
	ID     string // the row's UUID, what request_logs is summed by
	Name   string // for the alert text
	Budget Budget
}

// Subject kinds.
const (
	KindKey  = "key"
	KindUser = "user"
)

// SpendSource sums a subject's priced spend since an instant.
type SpendSource interface {
	Spend(ctx context.Context, kind, id string, since time.Time) (float64, error)
}

// PGSource sums request_logs.cost_usd. A key's rows carry its id; a user's
// carry the owner stamped when the row was written (the proxy stamps it on
// keyed rows and on the keyless dashboard chat alike), so what a user spent
// stays theirs when a key is handed to someone else or deleted.
type PGSource struct{ Pool *pgxpool.Pool }

// Spend implements SpendSource.
func (s PGSource) Spend(ctx context.Context, kind, id string, since time.Time) (float64, error) {
	col := "virtual_key_id"
	if kind == KindUser {
		col = "owner_user_id"
	}
	var spent float64
	err := s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM request_logs WHERE `+col+` = $1 AND created_at >= $2`,
		id, since).Scan(&spent)
	return spent, err
}

// refreshInterval is how long a summed figure is trusted before the source is
// asked again. Charges made through this member land in between, so a burst
// inside the window still counts; what the window hides is spend another
// member metered (budgets are per member, like the token limits).
const refreshInterval = time.Minute

// warnFraction is the share of a budget at which the warning event fires.
const warnFraction = 0.8

// sourceTimeout bounds one read of the source.
const sourceTimeout = 3 * time.Second

// entry is one subject's spend in the current period.
type entry struct {
	mu          sync.Mutex
	periodStart time.Time
	judged      Budget // the budget the flags were judged against
	spent       float64
	loadedAt    time.Time
	reloading   bool // a background reload is in flight
	warned      bool
	exceeded    bool
}

// Limiter refuses a request whose subject has spent its period's budget.
type Limiter struct {
	source  SpendSource
	entries sync.Map // kind+id -> *entry
	now     func() time.Time
}

// NewLimiter builds a limiter over the given spend source.
func NewLimiter(source SpendSource) *Limiter {
	return &Limiter{source: source, now: time.Now}
}

// Middleware refuses with 429 when a subject on the context has spent its
// budget. The request that crosses the line is served: what it costs is known
// only once the provider reports usage. Both subjects must pass, the key's and
// its owner's; a surface with no key (dashboard chat) carries the user's only.
// A read (the model listing) spends nothing and is never refused.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	if l == nil {
		return next // a handler built without the stage (tests) meters nothing
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		var refused *Subject
		var refusedSpent float64
		var until time.Time
		for _, key := range []any{ctxkeys.KeyBudgetKey, ctxkeys.UserBudgetKey} {
			s, _ := r.Context().Value(key).(*Subject)
			if s == nil {
				continue
			}
			spent, ok := l.admit(r.Context(), s)
			if ok {
				continue
			}
			// Both are judged, so the answer names the first refusal but waits
			// for the later period to end: that is when the request can succeed.
			if end := PeriodEnd(s.Budget.Period, l.now()); refused == nil || end.After(until) {
				until = end
			}
			if refused == nil {
				refused, refusedSpent = s, spent
			}
		}
		if refused != nil {
			httpx.SetRetryAfter(w, until.Sub(l.now()))
			util.WriteOpenAIError(w, fmt.Sprintf("%s budget exceeded: $%.2f of $%.2f spent this %s",
				refused.Kind, refusedSpent, refused.Budget.USD, refused.Budget.Period), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// admit reports the subject's period spend and whether it is under budget,
// firing the warning and exceeded events the first time each threshold is
// crossed in the period.
func (l *Limiter) admit(ctx context.Context, s *Subject) (float64, bool) {
	e := l.entry(s.Kind, s.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := l.now()
	l.refresh(ctx, s, e, now)
	if e.judged != s.Budget {
		// A budget edited mid-period is judged afresh: raising it above the
		// spend re-arms both events for the next crossing.
		e.judged, e.warned, e.exceeded = s.Budget, false, false
	}
	if e.spent >= s.Budget.USD {
		if !e.exceeded {
			e.exceeded = true
			publish("budget.exceeded", s, e.spent, now)
		}
		return e.spent, false
	}
	if !e.warned && e.spent >= warnFraction*s.Budget.USD {
		e.warned = true
		publish("budget.warning", s, e.spent, now)
	}
	return e.spent, true
}

// Spent reports a subject's spend in the current period for display. It reads
// the source when the figure is stale, the same way admission does.
func (l *Limiter) Spent(ctx context.Context, s *Subject) float64 {
	if l == nil {
		return 0
	}
	e := l.entry(s.Kind, s.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	l.refresh(ctx, s, e, l.now())
	return e.spent
}

// refresh makes the entry current for the period holding now. A rolled period
// starts from zero and is summed before the answer, and so is a subject seen
// for the first time; a figure that merely aged out is served as it stands
// while one background reload replaces it, so a slow store never holds the
// request path. Called with e.mu held; the first-sight read releases it for
// the length of the query so a charge for the subject is not held behind
// the store, and takes it back before it writes.
func (l *Limiter) refresh(ctx context.Context, s *Subject, e *entry, now time.Time) {
	start := PeriodStart(s.Budget.Period, now)
	if !e.periodStart.Equal(start) {
		// Field by field: the caller holds e.mu, and a struct assignment
		// would overwrite the mutex under it.
		e.periodStart, e.spent, e.loadedAt, e.reloading, e.warned, e.exceeded = start, 0, time.Time{}, false, false, false
	}
	if e.loadedAt.IsZero() {
		e.mu.Unlock()
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sourceTimeout)
		spent, err := l.source.Spend(qctx, s.Kind, s.ID, start)
		cancel()
		e.mu.Lock()
		if !e.periodStart.Equal(start) || !e.loadedAt.IsZero() {
			return // rolled, or summed by a concurrent first sight, meanwhile
		}
		if err != nil {
			// A budget is a spending cap, and a store that cannot be summed is
			// also one that records nothing to sum: the subject is admitted at
			// zero for one interval rather than refused, and the warning says
			// the cap is not being enforced.
			debuglog.Warn(logComponent+": could not sum spend, admitting unmetered until the next read",
				"kind", s.Kind, "name", s.Name, "error", err)
		}
		// A subject found already past a threshold when this process first
		// sums it (a restart inside the period, or the dashboard asking before
		// the first request) was reported by the process that watched it
		// cross; it is not reported again.
		e.spent, e.loadedAt, e.judged = spent, now, s.Budget
		e.warned = spent >= warnFraction*s.Budget.USD
		e.exceeded = spent >= s.Budget.USD
		return
	}
	if now.Sub(e.loadedAt) < refreshInterval || e.reloading {
		return
	}
	e.reloading = true
	// #nosec G118 -- intentional: the reload outlives the request that noticed
	// the figure was stale; it serves every later request for the subject.
	go l.reload(s, e, start)
}

// reload sums the subject again and replaces the cached figure, unless the
// period rolled meanwhile (the roll starts a fresh sum of its own). A failed
// read keeps the last known figure for another interval; either way the
// figure dates from when the read landed. Charges that land
// while the read is in flight are folded into the old figure and then
// replaced by the sum, which holds their rows if they were written before
// the read's snapshot and otherwise catches them next interval: the window
// is one query long and the miss is an undercount, the safe direction.
func (l *Limiter) reload(s *Subject, e *entry, start time.Time) {
	qctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	spent, err := l.source.Spend(qctx, s.Kind, s.ID, start)
	cancel()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reloading = false
	if !e.periodStart.Equal(start) {
		return
	}
	if err != nil {
		debuglog.Warn(logComponent+": could not sum spend, admitting on the last known figure",
			"kind", s.Kind, "name", s.Name, "error", err)
	} else {
		e.spent = spent
	}
	e.loadedAt = l.now()
}

// Charge adds a priced request to the key's and the owner's period spend so
// a burst inside the refresh window still counts. Called once the row is
// written; a reload that overlaps the write replaces the figure with a sum
// that already holds the row, so nothing is counted twice past the next
// interval. A subject never seen (no budget bound, or not yet admitted) is
// left alone; its figure is summed from the store on first use.
func (l *Limiter) Charge(keyID, ownerID string, cost float64) {
	if l == nil {
		return
	}
	l.charge(KindKey, keyID, cost)
	l.charge(KindUser, ownerID, cost)
}

func (l *Limiter) charge(kind, id string, cost float64) {
	if id == "" {
		return
	}
	v, ok := l.entries.Load(kind + ":" + id)
	if !ok {
		return
	}
	e := v.(*entry)
	e.mu.Lock()
	if !e.loadedAt.IsZero() {
		e.spent += cost
	}
	e.mu.Unlock()
}

func (l *Limiter) entry(kind, id string) *entry {
	v, _ := l.entries.LoadOrStore(kind+":"+id, &entry{})
	return v.(*entry)
}

func publish(kind string, s *Subject, spent float64, now time.Time) {
	end := PeriodEnd(s.Budget.Period, now)
	what := "virtual key"
	if s.Kind == KindUser {
		what = "user"
	}
	msg := fmt.Sprintf("%s %q has spent $%.2f of its $%.2f %s budget", what, s.Name, spent, s.Budget.USD, s.Budget.Period)
	severity := "warning"
	if kind == "budget.exceeded" {
		msg += "; requests are refused until " + end.Format("2006-01-02 15:04 UTC")
		severity = "error"
	}
	events.Publish(events.Event{
		Type:     kind,
		Severity: severity,
		Source:   logComponent,
		Message:  msg,
		Metadata: map[string]any{
			"subject":    s.Kind,
			"id":         s.ID,
			"name":       s.Name,
			"spent_usd":  spent,
			"budget_usd": s.Budget.USD,
			"period":     s.Budget.Period,
			"period_end": end.Format(time.RFC3339),
		},
	})
}
