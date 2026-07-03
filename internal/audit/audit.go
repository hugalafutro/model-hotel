// Package audit records an audit trail of admin actions: one row per mutating
// request (POST/PUT/PATCH/DELETE) on the authenticated dashboard API. Capture
// is middleware-based so new endpoints are covered without per-handler code.
// Request bodies are NEVER stored - they carry provider keys, passwords, and
// TOTP codes - only actor, method, route, entity id, response status, and the
// caller address. The table is instance-local operational telemetry (not
// fleet-synced, not in backups) and is pruned opportunistically after inserts
// against a configurable retention.
package audit

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// DefaultRetentionDays applies when the audit_retention_days setting is unset
// or invalid.
const DefaultRetentionDays = 90

// pruneInterval throttles the opportunistic retention sweep that piggybacks
// on inserts.
const pruneInterval = time.Hour

// Entry is one audited admin action.
type Entry struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	Actor      string    `json:"actor"`
	ActorRole  string    `json:"actor_role"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Path       string    `json:"path"`
	EntityID   string    `json:"entity_id,omitempty"`
	StatusCode int       `json:"status_code"`
	RemoteAddr string    `json:"remote_addr"`
}

// ListParams are the cursor-pagination and filter inputs for List.
type ListParams struct {
	// Cursor is the (CreatedAt, ID) boundary of the previous page; zero value
	// means "newest first from the top".
	CursorCreatedAt time.Time
	CursorID        string
	Limit           int
	Actor           string
	Method          string
	From            time.Time
	To              time.Time
}

// Recorder persists audit entries and prunes old ones. Wired over the shared
// Postgres pool.
type Recorder struct {
	pool *pgxpool.Pool
	// retentionDays returns the current retention window; read per prune so a
	// settings change applies without restart. Nil means DefaultRetentionDays.
	retentionDays func() int
	lastPruneUnix atomic.Int64
}

// New creates a Recorder. retentionDays may be nil (default retention).
func New(pool *pgxpool.Pool, retentionDays func() int) *Recorder {
	return &Recorder{pool: pool, retentionDays: retentionDays}
}

// actorOf renders the request identity for the audit row: the username for
// users-row identities, "admin" for every legacy admin login (env token,
// passkey, TOTP, SSO allowlist - they all share the fixed admin identity).
func actorOf(id *user.Identity) (actor, role string) {
	if id == nil {
		return "unknown", ""
	}
	if id.Username != "" {
		return id.Username, string(id.Role)
	}
	if id.IsAdmin() {
		return "admin", string(user.RoleAdmin)
	}
	return "unknown", string(id.Role)
}

// statusRecorder captures the response status the wrapped handler writes.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer (flushing
// SSE etc. keeps working through the wrapper).
func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Middleware records every mutating request passing through it. Mounted right
// after the auth middleware (identity present) and before the demo read-only
// guard, so refused attempts are recorded with their status too. Reads are
// never audited; neither are request bodies.
func (rec *Recorder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		sw := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		// Route pattern and URL params are populated once the router has
		// dispatched, so they are read after the handler ran.
		route, entityID := r.URL.Path, ""
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if p := rctx.RoutePattern(); p != "" {
				route = p
			}
			entityID = rctx.URLParam("id")
		}
		actor, role := actorOf(user.IdentityFrom(r.Context()))
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		rec.record(Entry{
			Actor:      actor,
			ActorRole:  role,
			Method:     r.Method,
			Route:      route,
			Path:       r.URL.Path,
			EntityID:   entityID,
			StatusCode: status,
			RemoteAddr: r.RemoteAddr,
		})
	})
}

// record inserts one entry, best-effort: an audit failure never fails the
// admin request it describes. It uses a background context so a client
// disconnect cannot lose the row, and piggybacks the throttled retention
// sweep.
func (rec *Recorder) record(e Entry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := rec.pool.Exec(ctx,
		`INSERT INTO audit_log (actor, actor_role, method, route, path, entity_id, status_code, remote_addr)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`,
		e.Actor, e.ActorRole, e.Method, e.Route, e.Path, e.EntityID, e.StatusCode, e.RemoteAddr)
	if err != nil {
		debuglog.Error("audit: failed to record entry", "error", err, "route", e.Route)
		return
	}
	rec.maybePrune(ctx)
}

// maybePrune runs the retention DELETE at most once per pruneInterval.
func (rec *Recorder) maybePrune(ctx context.Context) {
	now := time.Now().Unix()
	last := rec.lastPruneUnix.Load()
	if now-last < int64(pruneInterval.Seconds()) {
		return
	}
	if !rec.lastPruneUnix.CompareAndSwap(last, now) {
		return // another goroutine took this slot
	}
	days := DefaultRetentionDays
	if rec.retentionDays != nil {
		if d := rec.retentionDays(); d > 0 {
			days = d
		}
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	tag, err := rec.pool.Exec(ctx, `DELETE FROM audit_log WHERE created_at < $1`, cutoff)
	if err != nil {
		debuglog.Error("audit: retention prune failed", "error", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		debuglog.Info("audit: pruned old entries", "count", n, "retention_days", days)
	}
}

// List returns entries newest-first with keyset pagination, plus one extra row
// so the caller can detect has_more.
func (rec *Recorder) List(ctx context.Context, p ListParams) ([]Entry, error) {
	if p.Limit < 1 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	query := `SELECT id, created_at, actor, actor_role, method, route, path, COALESCE(entity_id, ''), status_code, remote_addr
		FROM audit_log WHERE 1=1`
	args := []any{}
	idx := 1
	add := func(frag string, val any) {
		query += fmt.Sprintf(frag, idx)
		args = append(args, val)
		idx++
	}
	if p.Actor != "" {
		add(" AND actor = $%d", p.Actor)
	}
	if p.Method != "" && isAuditedMethod(p.Method) {
		add(" AND method = $%d", strings.ToUpper(p.Method))
	}
	if !p.From.IsZero() {
		add(" AND created_at >= $%d", p.From)
	}
	if !p.To.IsZero() {
		add(" AND created_at <= $%d", p.To)
	}
	if !p.CursorCreatedAt.IsZero() && p.CursorID != "" {
		query += fmt.Sprintf(" AND (created_at < $%d OR (created_at = $%d AND id < $%d))", idx, idx+1, idx+2)
		args = append(args, p.CursorCreatedAt, p.CursorCreatedAt, p.CursorID)
		idx += 3
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", idx)
	args = append(args, p.Limit+1)

	rows, err := rec.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: list: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Actor, &e.ActorRole, &e.Method,
			&e.Route, &e.Path, &e.EntityID, &e.StatusCode, &e.RemoteAddr); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Count returns the total row count for the same filters (cursor excluded).
// Best-effort: 0 on error.
func (rec *Recorder) Count(ctx context.Context, p ListParams) int {
	query := `SELECT COUNT(*) FROM audit_log WHERE 1=1`
	args := []any{}
	idx := 1
	add := func(frag string, val any) {
		query += fmt.Sprintf(frag, idx)
		args = append(args, val)
		idx++
	}
	if p.Actor != "" {
		add(" AND actor = $%d", p.Actor)
	}
	if p.Method != "" && isAuditedMethod(p.Method) {
		add(" AND method = $%d", strings.ToUpper(p.Method))
	}
	if !p.From.IsZero() {
		add(" AND created_at >= $%d", p.From)
	}
	if !p.To.IsZero() {
		add(" AND created_at <= $%d", p.To)
	}
	var total int
	_ = rec.pool.QueryRow(ctx, query, args...).Scan(&total)
	return total
}

// Purge deletes entries older than cutoff, or everything when all is true.
func (rec *Recorder) Purge(ctx context.Context, cutoff time.Time, all bool) error {
	var err error
	if all {
		_, err = rec.pool.Exec(ctx, `DELETE FROM audit_log`)
	} else {
		_, err = rec.pool.Exec(ctx, `DELETE FROM audit_log WHERE created_at < $1`, cutoff)
	}
	if err != nil {
		return fmt.Errorf("audit: purge: %w", err)
	}
	return nil
}

// isAuditedMethod reports whether m is one of the recorded HTTP methods, so
// filter input cannot smuggle arbitrary values into the query (harmless with
// binds, but a bogus method can only ever match nothing).
func isAuditedMethod(m string) bool {
	switch strings.ToUpper(m) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}
