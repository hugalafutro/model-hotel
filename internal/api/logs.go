package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// CacheHits is an alias for the shared CacheHits type defined in util.
// The API uses this alias for clarity in LogEntry — the underlying type
// is the same one the proxy produces.
type CacheHits = util.CacheHits

// LogEntry represents a single request log entry.
type LogEntry struct {
	ID                        string     `json:"id"`
	ProviderID                string     `json:"provider_id"`
	ProviderName              string     `json:"provider_name"`
	ModelID                   string     `json:"model_id"`
	RequestHash               string     `json:"request_hash"`
	StatusCode                int        `json:"status_code"`
	LatencyMs                 float64    `json:"latency_ms"`
	DurationMs                float64    `json:"duration_ms"`
	TTFTMs                    float64    `json:"ttft_ms"`
	ResponseHeaderMs          float64    `json:"response_header_ms"`
	ProxyOverheadMs           float64    `json:"proxy_overhead_ms"`
	ParseMs                   float64    `json:"parse_ms"`
	FailoverLookupMs          float64    `json:"failover_lookup_ms"`
	ModelLookupMs             float64    `json:"model_lookup_ms"`
	ProviderLookupMs          float64    `json:"provider_lookup_ms"`
	KeyDecryptMs              float64    `json:"key_decrypt_ms"`
	DialMs                    float64    `json:"dial_ms"`
	SettingsReadMs            float64    `json:"settings_read_ms"`
	CacheHits                 *CacheHits `json:"cache_hits,omitempty"`
	TokensPerSecond           float64    `json:"tokens_per_second"`
	TokensPrompt              int        `json:"tokens_prompt"`
	TokensCompletion          int        `json:"tokens_completion"`
	TokensCompletionReasoning int        `json:"tokens_completion_reasoning"`
	TokensPromptCacheHit      int        `json:"tokens_prompt_cache_hit"`
	TokensPromptCacheMiss     int        `json:"tokens_prompt_cache_miss"`
	Streaming                 bool       `json:"streaming"`
	VirtualKeyName            string     `json:"virtual_key_name"`
	VirtualKeyDeleted         bool       `json:"virtual_key_deleted"`
	VirtualKeyID              string     `json:"virtual_key_id"`
	ClientIP                  string     `json:"client_ip"` // "" for rows predating migration 073 or address-less ingest paths
	ErrorMessage              string     `json:"error_message"`
	ErrorKind                 string     `json:"error_kind"` // "" when unclassified (legacy rows); frontend falls back to substring matching
	FailoverAttempt           int        `json:"failover_attempt"`
	State                     string     `json:"state"`
	CreatedAt                 time.Time  `json:"created_at"`
	ResolvedModelID           string     `json:"resolved_model_id"`
	EndpointType              string     `json:"endpoint_type"`
}

// LogsResponse is the paginated response for request logs.
type LogsResponse struct {
	Entries []LogEntry `json:"entries"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}

// RegisterLogs mounts log management routes.
func (h *Handler) RegisterLogs(r chi.Router) {
	r.Route("/logs", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireGrant(user.GrantLogs))
			r.Get("/", h.ListLogs)
			r.Get("/cursor", h.ListLogsCursor)
			r.Get("/{id}", h.GetLog)
		})
		// Purge is destructive and stays admin-only regardless of the grant.
		r.With(requireAdmin).Delete("/purge", h.PurgeLogs)
	})
}

// GetLog returns a single request log entry by ID.
func (h *Handler) GetLog(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "log ID")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Non-admins can only fetch their own rows; a non-owned id scans zero rows
	// and answers 404 below, so ownership is not an existence oracle. Same
	// two-shape disjunction as appendLogFilters — see the comment there.
	ownerPredicate := ""
	ownerArgs := []any{id}
	if scope := ownerScopeFromIdentity(r); scope != "" {
		ownerPredicate = " AND (rl.virtual_key_id IN (SELECT vko.id FROM virtual_keys vko WHERE vko.owner_user_id = $2)" +
			" OR (rl.virtual_key_id IS NULL AND rl.owner_user_id = $2))"
		ownerArgs = append(ownerArgs, scope)
	}

	var entry LogEntry
	err := h.dbPool.Pool().QueryRow(ctx, `
		SELECT rl.id, COALESCE(rl.provider_id::text, ''),
			CASE
				WHEN rl.provider_id IS NULL THEN ''
				WHEN p.name IS NOT NULL THEN p.name
				ELSE 'Deleted'
			END,
			rl.model_id,
			COALESCE(rl.request_hash, ''), COALESCE(rl.status_code, 0),
			COALESCE(rl.latency_ms, 0), COALESCE(rl.duration_ms, 0),
			COALESCE(rl.ttft_ms, 0), COALESCE(rl.proxy_overhead_ms, 0),
                COALESCE(rl.parse_ms, 0), COALESCE(rl.failover_lookup_ms, 0), COALESCE(rl.model_lookup_ms, 0), COALESCE(rl.provider_lookup_ms, 0), COALESCE(rl.key_decrypt_ms, 0),
                COALESCE(rl.dial_ms, 0), COALESCE(rl.settings_read_ms, 0),
                rl.cache_hits,
			COALESCE(rl.tokens_per_second, 0),
			COALESCE(rl.tokens_prompt, 0), COALESCE(rl.tokens_completion, 0),
			COALESCE(rl.tokens_completion_reasoning, 0),
			COALESCE(rl.tokens_prompt_cache_hit, 0), COALESCE(rl.tokens_prompt_cache_miss, 0),
			COALESCE(rl.streaming, false), COALESCE(rl.virtual_key_name, ''), COALESCE(rl.virtual_key_id::text, ''),
			 CASE
				WHEN rl.virtual_key_id IS NULL OR rl.virtual_key_id::text = '' THEN false
				WHEN vk.id IS NULL THEN true
				ELSE false
			END AS virtual_key_deleted,
			COALESCE(rl.error_message, ''), COALESCE(rl.failover_attempt, 0), COALESCE(rl.state, 'completed'), rl.created_at,
			COALESCE(rl.response_header_ms, 0),
			COALESCE(rl.resolved_model_id, ''),
			COALESCE(rl.endpoint_type, 'chat'),
			COALESCE(rl.error_kind, ''),
			COALESCE(rl.client_ip, '')
		FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
		LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
		WHERE rl.id = $1`+ownerPredicate,
		ownerArgs...,
	).Scan(
		&entry.ID, &entry.ProviderID, &entry.ProviderName, &entry.ModelID,
		&entry.RequestHash, &entry.StatusCode, &entry.LatencyMs, &entry.DurationMs,
		&entry.TTFTMs, &entry.ProxyOverheadMs,
		&entry.ParseMs, &entry.FailoverLookupMs, &entry.ModelLookupMs, &entry.ProviderLookupMs, &entry.KeyDecryptMs,
		&entry.DialMs, &entry.SettingsReadMs,
		&entry.CacheHits,
		&entry.TokensPerSecond,
		&entry.TokensPrompt, &entry.TokensCompletion, &entry.TokensCompletionReasoning,
		&entry.TokensPromptCacheHit, &entry.TokensPromptCacheMiss,
		&entry.Streaming,
		&entry.VirtualKeyName, &entry.VirtualKeyID, &entry.VirtualKeyDeleted,
		&entry.ErrorMessage,
		&entry.FailoverAttempt, &entry.State, &entry.CreatedAt,
		&entry.ResponseHeaderMs,
		&entry.ResolvedModelID,
		&entry.EndpointType,
		&entry.ErrorKind,
		&entry.ClientIP,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, "log not found", nil, http.StatusNotFound)
		} else {
			respondError(w, "failed to fetch log", err, http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, entry)
}

// ownerScopeFromIdentity returns the forced virtual-key-owner scope for the
// caller: non-admin identities only ever see traffic from keys they own, so
// their own user id is returned. Admins are unscoped (""). A non-admin
// without a users row cannot normally exist (resolveIdentity rejects it), but
// if one ever appears it scopes to uuid.Nil, which owns no keys - fail closed,
// not open.
func ownerScopeFromIdentity(r *http.Request) string {
	id := user.IdentityFrom(r.Context())
	if id == nil || id.IsAdmin() {
		return ""
	}
	if id.UserID == nil {
		return uuid.Nil.String()
	}
	return id.UserID.String()
}

// logOwnerScope resolves the owner filter for a log/stats listing: the forced
// identity scope for non-admins, or the optional ?owner_user_id=<uuid> filter
// for admins (ignored when unparseable, matching the other lenient filters).
func logOwnerScope(r *http.Request) string {
	if scope := ownerScopeFromIdentity(r); scope != "" {
		return scope
	}
	if v := r.URL.Query().Get("owner_user_id"); v != "" {
		if u, err := uuid.Parse(v); err == nil {
			return u.String()
		}
	}
	return ""
}

// LogsCursorResponse is the cursor-based paginated response for request logs.
type LogsCursorResponse struct {
	Entries   []LogEntry `json:"entries"`
	Total     int        `json:"total"`
	HasBefore bool       `json:"has_before"`
	HasAfter  bool       `json:"has_after"`
}

// ListLogsCursor returns request logs using keyset (cursor) pagination.
//
// Query parameters:
//   - cursor: encoded cursor from a previous response (base64 JSON of {created_at, id})
//   - direction: "after" (default) or "before" — which way to scroll from cursor
//   - limit: page size (default 20, max 200)
//   - model_id, provider_id, virtual_key_id, client_ip, status_code, from, to: same
//     filters as ListLogs
//   - sort_by: only "time" is supported for cursor pagination (default "time")
//   - sort_dir: "desc" (default, newest first) or "asc"
//
// The first request omits cursor to get the newest entries.
// Subsequent requests pass the cursor from the response boundary and
// direction to scroll older ("before") or newer ("after").
func (h *Handler) ListLogsCursor(w http.ResponseWriter, r *http.Request) {
	p, ok := parseLogListParams(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	query, args := buildLogListQuery(p)

	rows, err := h.dbPool.Pool().Query(ctx, query, args...)
	if err != nil {
		debuglog.Error("logs-cursor: failed to query logs", "error", err)
		respondError(w, "failed to query logs", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// limit is clamped to [1, 200] above; prealloc with the hard upper bound
	// to satisfy CodeQL's uncontrolled-allocation-size check (user input must
	// not flow into make() capacity even after clamping).
	entries := make([]LogEntry, 0, 201) // limit+1 for has_more detection
	for rows.Next() {
		entry, err := scanLogEntry(rows)
		if err != nil {
			debuglog.Error("logs-cursor: row scan failed", "error", err)
			continue
		}
		entries = append(entries, entry)
	}

	entries, hasAfter, hasBefore := paginateCursor(entries, p.direction, p.limit, p.cursorStr != "")

	response := LogsCursorResponse{
		Entries:   entries,
		Total:     h.countLogs(ctx, p),
		HasBefore: hasBefore,
		HasAfter:  hasAfter,
	}

	writeJSON(w, response)
}

// PurgeLogsRequest is the request body for purging logs.
type PurgeLogsRequest struct {
	OlderThan string `json:"older_than"`
}

// purgeOlderThanTokens is the human-readable list of accepted older_than
// values, reused in the 400 message by every purge endpoint.
const purgeOlderThanTokens = "1h, 1d, 1w, 1m, all"

// olderThanCutoff maps a purge range token to a cutoff time. all=true signals
// "delete everything" (cutoff is unused in that case); ok=false means the token
// was not recognized. Shared by the request-log and app-log purge endpoints so
// they accept exactly the same vocabulary.
func olderThanCutoff(olderThan string) (cutoff time.Time, all, ok bool) {
	switch olderThan {
	case "1h":
		return time.Now().Add(-1 * time.Hour), false, true
	case "1d":
		return time.Now().Add(-24 * time.Hour), false, true
	case "1w":
		return time.Now().Add(-7 * 24 * time.Hour), false, true
	case "1m":
		return time.Now().Add(-30 * 24 * time.Hour), false, true
	case "all":
		return time.Time{}, true, true
	default:
		return time.Time{}, false, false
	}
}

// PurgeLogs deletes old request logs based on the specified time range.
func (h *Handler) PurgeLogs(w http.ResponseWriter, r *http.Request) {
	var req PurgeLogsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	cutoff, all, ok := olderThanCutoff(req.OlderThan)
	if !ok {
		http.Error(w, "invalid older_than value, use: "+purgeOlderThanTokens, http.StatusBadRequest)
		return
	}

	if all {
		_, err := h.dbPool.Pool().Exec(r.Context(), `DELETE FROM request_logs`)
		if err != nil {
			respondError(w, "failed to purge logs", err, http.StatusInternalServerError)
			return
		}
		debuglog.Info("logs: purged all logs")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_, err := h.dbPool.Pool().Exec(r.Context(),
		`DELETE FROM request_logs WHERE created_at < $1`, cutoff)
	if err != nil {
		respondError(w, "failed to purge old logs", err, http.StatusInternalServerError)
		return
	}
	debuglog.Info("logs: purged old logs", "cutoff", cutoff)

	w.WriteHeader(http.StatusNoContent)
}

// ListLogs returns paginated request logs with filtering and sorting.
func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	page := max(util.GetIntQueryParam(r, "page", 1), 1)
	perPage := min(max(util.GetIntQueryParam(r, "per_page", 20), 1), 200)
	ownerUserID := logOwnerScope(r)
	// The response cache is shared across callers, so the key must carry the
	// owner scope: a non-admin page and the admin's unscoped page for the same
	// RawQuery are different result sets.
	cacheKey := ownerUserID + "|" + r.URL.RawQuery
	modelID := r.URL.Query().Get("model_id")
	providerID := r.URL.Query().Get("provider_id")
	virtualKeyID := r.URL.Query().Get("virtual_key_id")
	clientIP := r.URL.Query().Get("client_ip")
	statusCodeStr := r.URL.Query().Get("status_code")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")
	endpointType := r.URL.Query().Get("endpoint_type")
	sortBy, sd := logsSortDef(r.URL.Query().Get("sort_by"))
	sortDir := r.URL.Query().Get("sort_dir")
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "desc"
	}

	offset := (page - 1) * perPage

	if cached, ok := globalLogsCache.get(cacheKey); ok {
		w.Header().Set("X-Cache", "HIT")
		writeJSON(w, cached)
		return
	}

	query := "SELECT COUNT(*) OVER() AS total_count, " + logEntrySelectColumns

	args := []any{}
	argIndex := 1
	query, args, argIndex = appendLogFilters(query, args, argIndex, modelID, providerID, virtualKeyID, clientIP, statusCodeStr, fromDate, toDate, endpointType, ownerUserID)

	orderClause := " ORDER BY "
	if sd.tierExpr != "" {
		orderClause += sd.tierExpr + " ASC, "
	}
	orderClause += sd.valueExpr + " " + sortDir

	if sortBy == "status" {
		orderClause += ", CASE WHEN COALESCE(rl.error_message, '') ILIKE '%cancel%' OR COALESCE(rl.error_message, '') ILIKE '%disconnect%' OR COALESCE(rl.error_message, '') ILIKE '%context canceled%' THEN 1 ELSE 0 END ASC"
	}

	orderClause += " LIMIT $" + util.IntToStr(argIndex) + " OFFSET $" + util.IntToStr(argIndex+1)
	query += orderClause
	args = append(args, perPage, offset)

	rows, err := h.dbPool.Pool().Query(r.Context(), query, args...)
	if err != nil {
		debuglog.Error("logs: failed to query logs", "error", err)
		respondError(w, "failed to query logs", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]LogEntry, 0)
	var total int
	for rows.Next() {
		var entry LogEntry
		var totalCount int
		// Windowed COUNT(*) OVER() comes first; the rest is the shared projection.
		err := rows.Scan(append([]any{&totalCount}, logEntryScanDests(&entry)...)...)
		if err != nil {
			debuglog.Error("logs: row scan failed", "error", err)
			continue
		}
		if total == 0 {
			total = totalCount
		}
		entries = append(entries, entry)
	}

	response := LogsResponse{
		Entries: entries,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}

	globalLogsCache.set(cacheKey, &response)
	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, response)
}
