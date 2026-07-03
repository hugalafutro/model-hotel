package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/audit"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// SetAudit wires the audit recorder: its middleware records every mutating
// request on the authenticated API, and the admin-only /audit routes read the
// trail. Nil (tests without a recorder) disables both.
func (h *Handler) SetAudit(rec *audit.Recorder) {
	h.audit = rec
}

// RegisterAudit mounts the admin-only audit-trail routes.
func (h *Handler) RegisterAudit(r chi.Router) {
	r.Route("/audit", func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/", h.ListAudit)
		r.Delete("/purge", h.PurgeAudit)
	})
}

// AuditListResponse is the cursor-paginated audit page.
type AuditListResponse struct {
	Entries []audit.Entry `json:"entries"`
	Total   int           `json:"total"`
	HasMore bool          `json:"has_more"`
	// Cursor of the last returned row, to pass back for the next (older) page.
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListAudit returns audit entries newest-first with keyset pagination.
// Query params: cursor (base64 of {created_at,id}), limit (default 50, max
// 200), actor, method, from, to (RFC3339).
func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	if h.audit == nil {
		http.Error(w, "audit log is not available", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	p := audit.ListParams{
		Limit:  util.GetIntQueryParam(r, "limit", 50),
		Actor:  q.Get("actor"),
		Method: q.Get("method"),
	}
	if v := q.Get("from"); v != "" {
		if ts, err := time.Parse(time.RFC3339, v); err == nil {
			p.From = ts
		}
	}
	if v := q.Get("to"); v != "" {
		if ts, err := time.Parse(time.RFC3339, v); err == nil {
			p.To = ts
		}
	}
	if v := q.Get("cursor"); v != "" {
		var c logCursor
		if err := c.decode(v); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return
		}
		p.CursorCreatedAt = c.CreatedAt
		p.CursorID = c.ID
	}

	entries, err := h.audit.List(r.Context(), p)
	if err != nil {
		respondError(w, "failed to list audit entries", err, http.StatusInternalServerError)
		return
	}
	limit := p.Limit
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	resp := AuditListResponse{
		Entries: entries,
		Total:   h.audit.Count(r.Context(), p),
		HasMore: hasMore,
	}
	if resp.Entries == nil {
		resp.Entries = []audit.Entry{}
	}
	if hasMore && len(resp.Entries) > 0 {
		last := resp.Entries[len(resp.Entries)-1]
		c := logCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		resp.NextCursor = c.encode()
	}
	writeJSON(w, resp)
}

// PurgeAudit deletes old audit entries using the same older_than vocabulary
// as the request-log purge. The purge itself is a mutating request, so it is
// recorded by the audit middleware - a wiped trail always shows who wiped it.
func (h *Handler) PurgeAudit(w http.ResponseWriter, r *http.Request) {
	if h.audit == nil {
		http.Error(w, "audit log is not available", http.StatusNotFound)
		return
	}
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
	if err := h.audit.Purge(r.Context(), cutoff, all); err != nil {
		respondError(w, "failed to purge audit entries", err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
