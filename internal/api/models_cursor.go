package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// ListModelsCursor returns models using keyset (cursor) pagination.
//
// Query parameters:
//   - cursor: encoded cursor from a previous response
//   - direction: "after" (default) or "before"
//   - limit: page size (default 50, max 200)
//   - sort_by: "name" (default), "discovered", "context", "output", "provider", "status"
//   - sort_dir: "asc" (default) or "desc"
//   - search: text search on model_id, name, display_name
//   - provider_id: filter by provider UUID
//   - capabilities: comma-separated capability keys (e.g. "vision,reasoning")
func (h *Handler) ListModelsCursor(w http.ResponseWriter, r *http.Request) {
	if h.dbPool == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ModelsCursorResponse{})
		return
	}

	p, ok := parseModelListParams(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, cursorStr, direction, sortDir, sortBy := p.limit, p.cursorStr, p.direction, p.sortDir, p.sortBy

	// Build WHERE clause (shared with count query)
	filterConds, filterArgs := buildModelFilterConditions(q)
	conditions := filterConds
	args := filterArgs
	argIdx := len(args) + 1

	// Apply cursor keyset predicate
	if cursorStr != "" {
		pred := buildModelKeysetPredicate(p.cursor, direction, sortDir, &argIdx, &args)
		if pred != "" {
			conditions = append(conditions, pred)
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + joinAnd(conditions)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch entries (limit+1 to detect has_more)
	fetchLimit := limit + 1

	// Build ORDER BY based on sort_by
	// When paginating backward, invert the sort direction so LIMIT picks from
	// the correct end of the result set, then reverse the slice before returning.
	orderCol := modelSortColumn(sortBy)
	fetchSortDir := sortDir
	if direction == "before" {
		if fetchSortDir == "ASC" {
			fetchSortDir = "DESC"
		} else {
			fetchSortDir = "ASC"
		}
	}
	orderClause := fmt.Sprintf(" ORDER BY %s %s, m.id %s", orderCol, fetchSortDir, fetchSortDir)

	dataSQL := "SELECT " + modelSelectColumns + modelFromJoin +
		whereClause + orderClause + fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, fetchLimit)

	rows, err := h.dbPool.Pool().Query(ctx, dataSQL, args...)
	if err != nil {
		respondError(w, "failed to query models", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]ModelResponse, 0, limit)
	for rows.Next() {
		m, err := scanModelRow(rows)
		if err != nil {
			debuglog.Error("cursor row scan failed", "error", err)
			continue
		}
		entries = append(entries, modelToResponse(m))
	}

	// Determine has_after / has_before based on direction and fetched rows
	var hasAfter, hasBefore bool
	switch direction {
	case "after":
		if len(entries) > limit {
			hasAfter = true
			entries = entries[:limit]
		}
		if cursorStr != "" {
			hasBefore = true
		}
	case "before":
		if len(entries) > limit {
			hasBefore = true
			entries = entries[:limit]
		}
		if cursorStr != "" {
			hasAfter = true
		}
	}

	// Reverse entries for backward pagination: we fetched in inverted sort order
	// to get the correct window, but must return in the user's requested sort order.
	if direction == "before" {
		slices.Reverse(entries)
	}

	// Get total count (reuses same filter conditions, minus cursor predicate)
	totalCountConditions, totalCountArgs := buildModelFilterConditions(q)

	totalWhereClause := ""
	if len(totalCountConditions) > 0 {
		totalWhereClause = " WHERE " + joinAnd(totalCountConditions)
	}

	var total int
	_ = h.dbPool.Pool().QueryRow(ctx, "SELECT COUNT(*)"+modelFromJoin+totalWhereClause, totalCountArgs...).Scan(&total)

	writeJSON(w, ModelsCursorResponse{
		Entries:   entries,
		Total:     total,
		HasBefore: hasBefore,
		HasAfter:  hasAfter,
	})
}

// modelListParams holds the parsed, validated query inputs for ListModelsCursor.
type modelListParams struct {
	limit              int
	cursorStr          string
	cursor             modelCursor
	direction, sortDir string
	sortBy             string
}

// parseModelListParams reads and validates the cursor list query parameters:
// limit clamp ([1,200], default 50), direction (after default), sort_dir
// (ASC default), the sort_by whitelist, and the cursor (decode error → 400,
// with the cursor's own sort_by taking precedence for consistency).
func parseModelListParams(w http.ResponseWriter, r *http.Request) (modelListParams, bool) {
	q := r.URL.Query()
	p := modelListParams{
		limit:     50,
		cursorStr: q.Get("cursor"),
		direction: q.Get("direction"),
		sortDir:   "ASC",
		sortBy:    q.Get("sort_by"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			p.limit = n
		}
	}
	if p.direction != "before" && p.direction != "after" {
		p.direction = "after"
	}
	if q.Get("sort_dir") == "desc" {
		p.sortDir = "DESC"
	}
	switch p.sortBy {
	case "discovered", "context", "output", "provider", "status":
		// valid
	default:
		p.sortBy = "name"
	}
	if p.cursorStr != "" {
		if err := p.cursor.decode(p.cursorStr); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return p, false
		}
		// Use sort_by from cursor if present (ensures consistency).
		if p.cursor.SortBy != "" {
			p.sortBy = p.cursor.SortBy
		}
	}
	return p, true
}

// modelSelectColumns is the cursor data query's column projection (models joined
// to providers for p.name). Its order matches scanModelRow exactly.
const modelSelectColumns = "m.id, m.provider_id, m.model_id, COALESCE(m.name, ''), COALESCE(m.description, ''), COALESCE(m.display_name, ''), COALESCE(m.capabilities, '{}'), COALESCE(m.params, '{}'), COALESCE(m.modality, ''), COALESCE(m.input_modalities, '[]'), COALESCE(m.output_modalities, '[]'), m.context_length, m.max_output_tokens, m.input_price_per_million, m.input_price_per_million_cache_hit, m.output_price_per_million, COALESCE(m.owned_by, ''), m.enabled, m.disabled_manually, m.created_at, COALESCE(m.last_seen_at, m.created_at), p.name"

// modelFromJoin is the shared FROM/JOIN tail for the models cursor data and
// count queries.
const modelFromJoin = " FROM models m JOIN providers p ON m.provider_id = p.id"

// scanModelRow scans one row of the modelSelectColumns projection into a
// model.Model, so modelToResponse can map it — the same mapping ListModels uses.
func scanModelRow(rows pgx.Rows) (model.Model, error) {
	var m model.Model
	err := rows.Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName,
		&m.Capabilities, &m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName,
	)
	return m, err
}

// modelSortColumn returns the SQL column expression for a given sort_by value.
func modelSortColumn(sortBy string) string {
	switch sortBy {
	case "discovered":
		return "COALESCE(m.last_seen_at, m.created_at)"
	case "context":
		return "COALESCE(m.context_length, 0)"
	case "output":
		return "COALESCE(m.max_output_tokens, 0)"
	case "provider":
		return "COALESCE(p.name, '')"
	case "status":
		return "CASE WHEN m.enabled AND NOT m.disabled_manually THEN 0 WHEN m.enabled AND m.disabled_manually THEN 1 ELSE 2 END"
	default: // "name"
		return "COALESCE(m.name, m.model_id, '')"
	}
}

// buildModelKeysetPredicate builds the keyset WHERE clause for cursor pagination.
func buildModelKeysetPredicate(cursor modelCursor, direction, sortDir string, argIdx *int, args *[]interface{}) string {
	if cursor.ID == "" {
		return ""
	}

	// For DESC sort + "after" (older): WHERE (col, id) < (cursor_val, cursor_id)
	// For DESC sort + "before" (newer): WHERE (col, id) > (cursor_val, cursor_id)
	// For ASC sort + "after" (next): WHERE (col, id) > (cursor_val, cursor_id)
	// For ASC sort + "before" (prev): WHERE (col, id) < (cursor_val, cursor_id)
	op := ">"
	if direction == "after" && sortDir == "DESC" {
		op = "<"
	} else if direction == "before" && sortDir == "ASC" {
		op = "<"
	}

	switch cursor.SortBy {
	case "discovered":
		if !cursor.LastSeenAt.IsZero() {
			pred := fmt.Sprintf("(COALESCE(m.last_seen_at, m.created_at), m.id) %s ($%d, $%d)", op, *argIdx, *argIdx+1)
			*args = append(*args, cursor.LastSeenAt, cursor.ID)
			*argIdx += 2
			return pred
		}
	case "context":
		if cursor.ContextLength != nil {
			pred := fmt.Sprintf("(COALESCE(m.context_length, 0), m.id) %s ($%d, $%d)", op, *argIdx, *argIdx+1)
			*args = append(*args, *cursor.ContextLength, cursor.ID)
			*argIdx += 2
			return pred
		}
	case "output":
		if cursor.MaxOutput != nil {
			pred := fmt.Sprintf("(COALESCE(m.max_output_tokens, 0), m.id) %s ($%d, $%d)", op, *argIdx, *argIdx+1)
			*args = append(*args, *cursor.MaxOutput, cursor.ID)
			*argIdx += 2
			return pred
		}
	case "provider":
		if cursor.ProviderName != "" {
			pred := fmt.Sprintf("(COALESCE(p.name, ''), m.id) %s ($%d, $%d)", op, *argIdx, *argIdx+1)
			*args = append(*args, cursor.ProviderName, cursor.ID)
			*argIdx += 2
			return pred
		}
	case "status":
		if cursor.StatusSort != nil {
			pred := fmt.Sprintf("(CASE WHEN m.enabled AND NOT m.disabled_manually THEN 0 WHEN m.enabled AND m.disabled_manually THEN 1 ELSE 2 END, m.id) %s ($%d, $%d)", op, *argIdx, *argIdx+1)
			*args = append(*args, *cursor.StatusSort, cursor.ID)
			*argIdx += 2
			return pred
		}
	default: // "name"
		name := cursor.Name
		if name == "" {
			name = cursor.ModelID
		}
		pred := fmt.Sprintf("(COALESCE(m.name, m.model_id, ''), m.id) %s ($%d, $%d)", op, *argIdx, *argIdx+1)
		*args = append(*args, name, cursor.ID)
		*argIdx += 2
		return pred
	}

	return ""
}

// splitComma splits a comma-separated string, trimming whitespace from each element.
func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// joinAnd joins conditions with AND. Returns empty string for empty slice.
func joinAnd(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return strings.Join(conditions, " AND ")
}

// buildModelFilterConditions builds the WHERE clause conditions and args for
// search, provider_id, and capabilities filters. Shared between the main data
// query and the count query to avoid duplication.
func buildModelFilterConditions(q url.Values) ([]string, []interface{}) {
	conditions := []string{}
	args := []interface{}{}
	argIdx := 1

	if search := q.Get("search"); search != "" {
		conditions = append(conditions, fmt.Sprintf(
			"(m.model_id ILIKE $%d OR COALESCE(m.name, '') ILIKE $%d OR COALESCE(m.display_name, '') ILIKE $%d)",
			argIdx, argIdx, argIdx,
		))
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if providerIDs := q.Get("provider_id"); providerIDs != "" {
		pids := splitComma(providerIDs)
		if len(pids) > 0 {
			validPids := make([]uuid.UUID, 0, len(pids))
			for _, pidStr := range pids {
				if pid, err := uuid.Parse(pidStr); err == nil {
					validPids = append(validPids, pid)
				}
			}
			if len(validPids) == 1 {
				conditions = append(conditions, fmt.Sprintf("m.provider_id = $%d", argIdx))
				args = append(args, validPids[0])
				argIdx++
			} else if len(validPids) > 1 {
				placeholders := make([]string, 0, len(validPids))
				for _, pid := range validPids {
					placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
					args = append(args, pid)
					argIdx++
				}
				conditions = append(conditions, fmt.Sprintf("m.provider_id IN (%s)", strings.Join(placeholders, ", ")))
			}
		}
	}
	if caps := q.Get("capabilities"); caps != "" {
		capMap := map[string]bool{}
		for _, c := range splitComma(caps) {
			if c != "" {
				capMap[c] = true
			}
		}
		if len(capMap) > 0 {
			capJSON, _ := json.Marshal(capMap)
			conditions = append(conditions, fmt.Sprintf("COALESCE(m.capabilities, '{}')::jsonb @> $%d::jsonb", argIdx))
			args = append(args, string(capJSON))
		}
	}

	return conditions, args
}
