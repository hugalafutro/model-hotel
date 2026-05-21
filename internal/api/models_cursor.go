package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			limit = n
		}
	}
	cursorStr := q.Get("cursor")
	direction := q.Get("direction")
	if direction != "before" && direction != "after" {
		direction = "after"
	}
	sortDir := "ASC"
	if q.Get("sort_dir") == "desc" {
		sortDir = "DESC"
	}
	sortBy := q.Get("sort_by")
	switch sortBy {
	case "discovered", "context", "output", "provider", "status":
		// valid
	default:
		sortBy = "name"
	}

	// Parse cursor
	var cursor modelCursor
	if cursorStr != "" {
		if err := cursor.decode(cursorStr); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return
		}
		// Use sort_by from cursor if present (ensures consistency)
		if cursor.SortBy != "" {
			sortBy = cursor.SortBy
		}
	}

	// Build WHERE clause
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
	if providerID := q.Get("provider_id"); providerID != "" {
		if pid, err := uuid.Parse(providerID); err == nil {
			conditions = append(conditions, fmt.Sprintf("m.provider_id = $%d", argIdx))
			args = append(args, pid)
			argIdx++
		}
	}
	if caps := q.Get("capabilities"); caps != "" {
		// Build a JSON object for @> containment check
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
			argIdx++
		}
	}

	// Apply cursor keyset predicate
	if cursorStr != "" {
		pred := buildModelKeysetPredicate(cursor, direction, sortDir, &argIdx, &args)
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
	orderCol := modelSortColumn(sortBy)
	orderClause := fmt.Sprintf(" ORDER BY %s %s, m.id %s", orderCol, sortDir, sortDir)

	dataSQL := "SELECT m.id, m.provider_id, m.model_id, COALESCE(m.name, ''), COALESCE(m.description, ''), COALESCE(m.display_name, ''), COALESCE(m.capabilities, '{}'), COALESCE(m.params, '{}'), COALESCE(m.modality, ''), COALESCE(m.input_modalities, '[]'), COALESCE(m.output_modalities, '[]'), m.context_length, m.max_output_tokens, m.input_price_per_million, m.input_price_per_million_cache_hit, m.output_price_per_million, COALESCE(m.owned_by, ''), m.enabled, m.disabled_manually, m.created_at, COALESCE(m.last_seen_at, m.created_at), p.name, p.enabled FROM models m JOIN providers p ON m.provider_id = p.id" +
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
		var m struct {
			ID                           uuid.UUID
			ProviderID                   uuid.UUID
			ModelID                      string
			Name                         string
			Description                  string
			DisplayName                  string
			Capabilities                 string
			Params                       string
			Modality                     string
			InputModalities              string
			OutputModalities             string
			ContextLength                *int
			MaxOutputTokens              *int
			InputPricePerMillion         *float64
			InputPricePerMillionCacheHit *float64
			OutputPricePerMillion        *float64
			OwnedBy                      string
			Enabled                      bool
			DisabledManually             bool
			CreatedAt                    time.Time
			LastSeenAt                   time.Time
			ProviderName                 string
			ProviderEnabled              bool
		}
		if err := rows.Scan(
			&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName,
			&m.Capabilities, &m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
			&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
			&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
		); err != nil {
			debuglog.Error("cursor row scan failed", "error", err)
			continue
		}
		entries = append(entries, ModelResponse{
			ID:                           m.ID.String(),
			ModelID:                      m.ModelID,
			Name:                         m.Name,
			Description:                  m.Description,
			DisplayName:                  m.DisplayName,
			ProviderID:                   m.ProviderID.String(),
			ProviderName:                 m.ProviderName,
			Capabilities:                 m.Capabilities,
			Params:                       m.Params,
			Modality:                     m.Modality,
			InputModalities:              m.InputModalities,
			OutputModalities:             m.OutputModalities,
			ContextLength:                m.ContextLength,
			MaxOutputTokens:              m.MaxOutputTokens,
			InputPricePerMillion:         m.InputPricePerMillion,
			InputPricePerMillionCacheHit: m.InputPricePerMillionCacheHit,
			OutputPricePerMillion:        m.OutputPricePerMillion,
			OwnedBy:                      m.OwnedBy,
			Enabled:                      m.Enabled,
			DisabledManually:             m.DisabledManually,
			CreatedAt:                    m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			LastSeenAt:                   m.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
		})
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
	}

	// Get total count (separate lightweight query)
	totalCountConditions := []string{}
	totalCountArgs := []interface{}{}
	totalCountArgIdx := 1

	if search := q.Get("search"); search != "" {
		totalCountConditions = append(totalCountConditions, fmt.Sprintf(
			"(m.model_id ILIKE $%d OR COALESCE(m.name, '') ILIKE $%d OR COALESCE(m.display_name, '') ILIKE $%d)",
			totalCountArgIdx, totalCountArgIdx, totalCountArgIdx,
		))
		totalCountArgs = append(totalCountArgs, "%"+search+"%")
		totalCountArgIdx++
	}
	if providerID := q.Get("provider_id"); providerID != "" {
		if pid, err := uuid.Parse(providerID); err == nil {
			totalCountConditions = append(totalCountConditions, fmt.Sprintf("m.provider_id = $%d", totalCountArgIdx))
			totalCountArgs = append(totalCountArgs, pid)
			totalCountArgIdx++
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
			totalCountConditions = append(totalCountConditions, fmt.Sprintf("COALESCE(m.capabilities, '{}')::jsonb @> $%d::jsonb", totalCountArgIdx))
			totalCountArgs = append(totalCountArgs, string(capJSON))
		}
	}

	totalWhereClause := ""
	if len(totalCountConditions) > 0 {
		totalWhereClause = " WHERE " + joinAnd(totalCountConditions)
	}

	var total int
	_ = h.dbPool.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM models m"+totalWhereClause, totalCountArgs...).Scan(&total)

	writeJSON(w, ModelsCursorResponse{
		Entries:   entries,
		Total:     total,
		HasBefore: hasBefore,
		HasAfter:  hasAfter,
	})
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
