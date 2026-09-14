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

// PGSource sums request_logs.cost_usd. A key's rows carry its id; a user's are
// the rows their keys wrote plus the keyless rows (dashboard chat) that carry
// the owner directly.
type PGSource struct{ Pool *pgxpool.Pool }

// Spend implements SpendSource.
func (s PGSource) Spend(ctx context.Context, kind, id string, since time.Time) (float64, error) {
	q := `SELECT COALESCE(SUM(cost_usd), 0) FROM request_logs WHERE virtual_key_id = $1 AND created_at >= $2`
	if kind == KindUser {
		q = `SELECT COALESCE(SUM(cost_usd), 0) FROM request_logs
		     WHERE created_at >= $2
		       AND (owner_user_id = $1 OR virtual_key_id IN (SELECT id FROM virtual_keys WHERE owner_user_id = $1))`
	}
	var spent float64
	err := s.Pool.QueryRow(ctx, q, id, since).Scan(&spent)
	return spent, err
}

// refreshInterval is how long a summed figure is trusted before the source is
// asked again. Charges made through this member land in between, so a burst
// inside the window still counts; what the window hides is spend another
// member metered (budgets are per member, like the token limits).
const refreshInterval = time.Minute

// warnFraction is the share of a budget at which the warning event fires.
const warnFraction = 0.8

// entry is one subject's spend in the current period.
type entry struct {
	mu          sync.Mutex
	periodStart time.Time
	spent       float64
	loadedAt    time.Time
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
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	if l == nil {
		return next // a handler built without the stage (tests) meters nothing
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []any{ctxkeys.KeyBudgetKey, ctxkeys.UserBudgetKey} {
			s, _ := r.Context().Value(key).(*Subject)
			if s == nil {
				continue
			}
			if spent, ok := l.admit(r.Context(), s); !ok {
				httpx.SetRetryAfter(w, PeriodEnd(s.Budget.Period, l.now()).Sub(l.now()))
				util.WriteOpenAIError(w, fmt.Sprintf("%s budget exceeded: $%.2f of $%.2f spent this %s",
					s.Kind, spent, s.Budget.USD, s.Budget.Period), http.StatusTooManyRequests)
				return
			}
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
	e := l.entry(s.Kind, s.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	l.refresh(ctx, s, e, l.now())
	return e.spent
}

// refresh reloads the entry from the source when the period rolled or the
// figure aged out. A source failure keeps what is known (or nothing, at
// first sight) and lets the request through: a budget is a spending cap, and
// a store that cannot be summed cannot be charged either.
func (l *Limiter) refresh(ctx context.Context, s *Subject, e *entry, now time.Time) {
	start := PeriodStart(s.Budget.Period, now)
	if !e.periodStart.Equal(start) {
		// Field by field: the caller holds e.mu, and a struct assignment
		// would overwrite the mutex under it.
		e.periodStart, e.spent, e.loadedAt, e.warned, e.exceeded = start, 0, time.Time{}, false, false
	}
	if !e.loadedAt.IsZero() && now.Sub(e.loadedAt) < refreshInterval {
		return
	}
	qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	spent, err := l.source.Spend(qctx, s.Kind, s.ID, start)
	if err != nil {
		debuglog.Warn(logComponent+": could not sum spend, admitting on the last known figure",
			"kind", s.Kind, "name", s.Name, "error", err)
		e.loadedAt = now // do not hammer a failing store; retry next interval
		return
	}
	e.spent, e.loadedAt = spent, now
}

// Charge adds a priced request to the key's and the owner's period spend so
// a burst inside the refresh window still counts. Called before the row is
// written: a reload between the two undercounts until the row lands, which
// is the safe direction. A subject never seen (no budget bound, or not yet
// admitted) is left alone; its figure is summed from the store on first use.
func (l *Limiter) Charge(keyID, ownerID string, cost float64) {
	if l == nil {
		return
	}
	for kind, id := range map[string]string{KindKey: keyID, KindUser: ownerID} {
		if id == "" {
			continue
		}
		v, ok := l.entries.Load(kind + ":" + id)
		if !ok {
			continue
		}
		e := v.(*entry)
		e.mu.Lock()
		if !e.loadedAt.IsZero() {
			e.spent += cost
		}
		e.mu.Unlock()
	}
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
