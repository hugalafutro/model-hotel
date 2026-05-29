package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// FailoverHandler handles failover group API endpoints.
type FailoverHandler struct {
	failoverRepo *failover.Repository
	modelRepo    *model.Repository
	dbPool       *pgxpool.Pool
	settingsRepo SettingsStore
}

// NewFailoverHandler creates a new failover group handler.
func NewFailoverHandler(dbPool *pgxpool.Pool, failoverRepo *failover.Repository, modelRepo *model.Repository, settingsRepo SettingsStore) *FailoverHandler {
	return &FailoverHandler{
		failoverRepo: failoverRepo,
		modelRepo:    modelRepo,
		dbPool:       dbPool,
		settingsRepo: settingsRepo,
	}
}

// FailoverEntryResponse represents a failover group entry in API responses.
type FailoverEntryResponse struct {
	ModelUUID     string `json:"model_uuid"`
	ModelID       string `json:"model_id"`
	ProviderID    string `json:"provider_id"`
	ProviderName  string `json:"provider_name"`
	DisplayName   string `json:"display_name"`
	Enabled       bool   `json:"enabled"`
	ContextLength *int   `json:"context_length"`
	OwnedBy       string `json:"owned_by"`
}

// FailoverGroupResponse represents a failover group in API responses.
type FailoverGroupResponse struct {
	ID           string                  `json:"id"`
	DisplayModel string                  `json:"display_model"`
	DisplayName  *string                 `json:"display_name"`
	Description  string                  `json:"description"`
	GroupEnabled bool                    `json:"group_enabled"`
	AutoCreated  bool                    `json:"auto_created"`
	Entries      []FailoverEntryResponse `json:"entries"`
	TotalTokens  int                     `json:"total_tokens"`
	CreatedAt    string                  `json:"created_at"`
	UpdatedAt    string                  `json:"updated_at"`
}

// FailoverListResponse is the response for listing failover groups.
type FailoverListResponse struct {
	Groups       []FailoverGroupResponse `json:"groups"`
	LastSyncedAt *string                 `json:"last_synced_at"`
}

// FailoverGroupBrief contains brief failover group info for list views.
type FailoverGroupBrief struct {
	ID           string `json:"id"`
	DisplayModel string `json:"display_model"`
	Position     int    `json:"position"`
	TotalEntries int    `json:"total_entries"`
}

// Register mounts failover group routes on the given router.
func (h *FailoverHandler) Register(r chi.Router) {
	r.Route("/failover-groups", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/sync", h.Sync)
		r.Get("/candidates", h.Candidates)
		r.Get("/by-model/{model_uuid}", h.GetByModelUUID)
		r.Get("/{id}", h.Get)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

// List returns all failover groups.
func (h *FailoverHandler) List(w http.ResponseWriter, r *http.Request) {
	groups, err := h.failoverRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list failover groups", err, http.StatusInternalServerError)
		return
	}

	tokenCounts, err := h.getTokenCounts(r.Context())
	if err != nil {
		tokenCounts = make(map[string]int)
	}

	responses := make([]FailoverGroupResponse, len(groups))
	for i, g := range groups {
		resp, err := h.buildGroupResponse(r.Context(), g)
		if err != nil {
			respondError(w, fmt.Sprintf("failed to build response for failover group %s", g.DisplayModel), err, http.StatusInternalServerError)
			return
		}
		resp.TotalTokens = tokenCounts["hotel/"+strings.ToLower(g.DisplayModel)]
		responses[i] = resp
	}

	lastSyncedAt := h.settingsRepo.GetWithDefault(r.Context(), "failover_last_synced_at", "")

	var lastSyncedAtPtr *string
	if lastSyncedAt != "" {
		lastSyncedAtPtr = &lastSyncedAt
	}

	writeJSON(w, FailoverListResponse{
		Groups:       responses,
		LastSyncedAt: lastSyncedAtPtr,
	})
}

func (h *FailoverHandler) getTokenCounts(ctx context.Context) (map[string]int, error) {
	rows, err := h.dbPool.Query(ctx, `
		SELECT LOWER(model_id), SUM(COALESCE(tokens_prompt, 0) + COALESCE(tokens_completion, 0)) as total_tokens
		FROM request_logs
		WHERE model_id ILIKE 'hotel/%' AND created_at > now() - interval '30 days'
		GROUP BY LOWER(model_id)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var modelID string
		var total int
		if err := rows.Scan(&modelID, &total); err != nil {
			continue
		}
		counts[modelID] = total
	}
	return counts, nil
}

// Get retrieves a failover group by ID.
func (h *FailoverHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "failover group ID")
	if !ok {
		return
	}

	g, err := h.failoverRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "failover group not found", http.StatusNotFound)
		return
	}

	resp, err := h.buildGroupResponse(r.Context(), g)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to build response for failover group %s", id), err, http.StatusInternalServerError)
		return
	}

	tokenCounts, err := h.getTokenCounts(r.Context())
	if err != nil {
		tokenCounts = make(map[string]int)
	}
	resp.TotalTokens = tokenCounts["hotel/"+strings.ToLower(g.DisplayModel)]

	writeJSON(w, resp)
}

// CreateFailoverGroupRequest is the request body for creating a failover group.
type CreateFailoverGroupRequest struct {
	DisplayModel string   `json:"display_model"`
	DisplayName  *string  `json:"display_name"`
	Description  *string  `json:"description"`
	EntryIDs     []string `json:"entry_ids"`
}

// Create creates a new failover group.
func (h *FailoverHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateFailoverGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	trimmedModel, err := validateNameString("display_model", req.DisplayModel, 1, 128)
	if err != nil {
		respondBadRequest(w, "invalid display model", err)
		return
	}
	req.DisplayModel = strings.ToLower(trimmedModel)

	dn, dnErr := validateNamePtr("display_name", req.DisplayName, 1, 128)
	if dnErr != nil {
		respondBadRequest(w, "invalid display name", dnErr)
		return
	}
	req.DisplayName = dn

	if err := validateStringPtrLength("description", req.Description, 0, 500); err != nil {
		respondBadRequest(w, "invalid description", err)
		return
	}

	if len(req.EntryIDs) < 2 {
		http.Error(w, "at least 2 entries required for failover group", http.StatusBadRequest)
		return
	}

	priorityOrder := make([]uuid.UUID, len(req.EntryIDs))
	for i, idStr := range req.EntryIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid entry_id: "+idStr, http.StatusBadRequest)
			return
		}
		priorityOrder[i] = id
	}

	entryEnabled := make(map[string]bool)
	for _, id := range priorityOrder {
		entryEnabled[id.String()] = true
	}

	existing, _ := h.failoverRepo.GetByModel(r.Context(), req.DisplayModel)
	if existing != nil {
		http.Error(w, "failover group with display_model '"+req.DisplayModel+"' already exists", http.StatusConflict)
		return
	}

	autoCreated := false
	group, err := h.failoverRepo.UpsertWithConfig(r.Context(), req.DisplayModel, priorityOrder,
		entryEnabled, nil, req.DisplayName, req.Description, &autoCreated)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to create failover group %q", req.DisplayModel), err, http.StatusInternalServerError)
		return
	}

	resp, err := h.buildGroupResponse(r.Context(), group)
	if err != nil {
		respondError(w, "failed to build failover group response", err, http.StatusInternalServerError)
		return
	}

	writeJSONCreated(w, resp)
}

// UpdateFailoverGroupRequest is the request body for updating a failover group.
type UpdateFailoverGroupRequest struct {
	DisplayName   *string         `json:"display_name"`
	Description   *string         `json:"description"`
	DisplayModel  *string         `json:"display_model"`
	GroupEnabled  *bool           `json:"group_enabled"`
	PriorityOrder []string        `json:"priority_order"`
	EntryEnabled  map[string]bool `json:"entry_enabled"`
}

// Update updates an existing failover group by ID.
func (h *FailoverHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "failover group ID")
	if !ok {
		return
	}

	var req UpdateFailoverGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	existing, err := h.failoverRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "failover group not found", http.StatusNotFound)
		return
	}

	// Validate display_model if provided
	if req.DisplayModel != nil {
		trimmedModel, modelErr := validateNameString("display_model", *req.DisplayModel, 1, 128)
		if modelErr != nil {
			respondBadRequest(w, "invalid display model", modelErr)
			return
		}
		lowerModel := strings.ToLower(trimmedModel)
		req.DisplayModel = &lowerModel

		// Uniqueness check: no other failover group should have this display_model
		if *req.DisplayModel != existing.DisplayModel {
			conflict, err := h.failoverRepo.GetByModel(r.Context(), *req.DisplayModel)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				respondError(w, "failed to check display_model uniqueness", err, http.StatusInternalServerError)
				return
			}
			if conflict != nil {
				http.Error(w, "failover group with display_model '"+*req.DisplayModel+"' already exists", http.StatusConflict)
				return
			}
		}
	}

	// Validate field lengths
	dn, dnErr := validateNamePtr("display_name", req.DisplayName, 1, 128)
	if dnErr != nil {
		respondBadRequest(w, "invalid display name", dnErr)
		return
	}
	req.DisplayName = dn

	if err := validateStringPtrLength("description", req.Description, 0, 500); err != nil {
		respondBadRequest(w, "invalid description", err)
		return
	}

	if req.EntryEnabled != nil {
		if err := validateMapSize("entry_enabled", req.EntryEnabled, 100); err != nil {
			respondBadRequest(w, "invalid entry_enabled", err)
			return
		}
	}

	priorityOrder := existing.PriorityOrder
	entryEnabled := existing.EntryEnabled

	if req.PriorityOrder != nil {
		priorityOrder = make([]uuid.UUID, len(req.PriorityOrder))
		for i, idStr := range req.PriorityOrder {
			parsedID, err := uuid.Parse(idStr)
			if err != nil {
				http.Error(w, "invalid priority_order entry: "+idStr, http.StatusBadRequest)
				return
			}
			priorityOrder[i] = parsedID
		}
	}

	if req.EntryEnabled != nil {
		entryEnabled = req.EntryEnabled
	}

	// Determine the effective group_enabled value
	effectiveGroupEnabled := existing.GroupEnabled
	if req.GroupEnabled != nil {
		effectiveGroupEnabled = *req.GroupEnabled
	}

	// Validate that at least one entry is enabled for an active failover group
	if effectiveGroupEnabled {
		hasEnabled := false
		for _, enabled := range entryEnabled {
			if enabled {
				hasEnabled = true
				break
			}
		}
		if !hasEnabled {
			http.Error(w, "at least one entry must be enabled for an active failover group", http.StatusBadRequest)
			return
		}
	}

	// Always invalidate cache so priority reorders and entry changes take
	// effect on the next request instead of waiting for the 5-minute TTL.
	failover.InvalidateFailoverCacheKey(existing.DisplayModel)

	// Also invalidate the new key if display_model is being renamed.
	if req.DisplayModel != nil && *req.DisplayModel != existing.DisplayModel {
		failover.InvalidateFailoverCacheKey(*req.DisplayModel)
	}

	group, err := h.failoverRepo.Update(r.Context(), id, priorityOrder, entryEnabled,
		req.GroupEnabled, req.DisplayName, req.Description, req.DisplayModel)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to update failover group %s", id), err, http.StatusInternalServerError)
		return
	}

	resp, err := h.buildGroupResponse(r.Context(), group)
	if err != nil {
		respondError(w, "failed to build failover group response", err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, resp)
}

// Delete deletes a failover group by ID.
func (h *FailoverHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "failover group ID")
	if !ok {
		return
	}

	if err := h.failoverRepo.DeleteByID(r.Context(), id); err != nil {
		respondError(w, fmt.Sprintf("failed to delete failover group %s", id), err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Sync synchronizes failover groups with model database.
func (h *FailoverHandler) Sync(w http.ResponseWriter, r *http.Request) {
	result, err := h.failoverRepo.SyncAllModels(r.Context())
	if err != nil {
		respondError(w, "failed to sync failover groups", err, http.StatusInternalServerError)
		return
	}

	if err := h.settingsRepo.Set(r.Context(), "failover_last_synced_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		debuglog.Debug("failover: failed to persist last_synced_at", "error", err)
	}

	writeJSON(w, result)
}

// CandidateModelResponse represents a model candidate for failover groups.
type CandidateModelResponse struct {
	ModelUUID     string `json:"model_uuid"`
	ModelID       string `json:"model_id"`
	ProviderID    string `json:"provider_id"`
	ProviderName  string `json:"provider_name"`
	DisplayName   string `json:"display_name"`
	ContextLength *int   `json:"context_length"`
	OwnedBy       string `json:"owned_by"`
}

// Candidates returns available models that can be added to failover groups.
func (h *FailoverHandler) Candidates(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelRepo.List(r.Context(), nil)
	if err != nil {
		respondError(w, "failed to list model candidates", err, http.StatusInternalServerError)
		return
	}

	candidates := make([]CandidateModelResponse, 0, len(models))
	for _, m := range models {
		if !m.Enabled || !m.ProviderEnabled {
			continue
		}
		candidates = append(candidates, CandidateModelResponse{
			ModelUUID:     m.ID.String(),
			ModelID:       m.ModelID,
			ProviderID:    m.ProviderID.String(),
			ProviderName:  m.ProviderName,
			DisplayName:   m.DisplayName,
			ContextLength: m.ContextLength,
			OwnedBy:       m.OwnedBy,
		})
	}

	writeJSON(w, candidates)
}

// GetByModelUUID retrieves a failover group by model UUID.
func (h *FailoverHandler) GetByModelUUID(w http.ResponseWriter, r *http.Request) {
	modelUUID, ok := parseUUIDParam(w, r, "model_uuid", "model UUID")
	if !ok {
		return
	}

	groups, err := h.failoverRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list failover groups", err, http.StatusInternalServerError)
		return
	}

	for _, g := range groups {
		for i, entryUUID := range g.PriorityOrder {
			if entryUUID == modelUUID {
				resp := FailoverGroupBrief{
					ID:           g.ID.String(),
					DisplayModel: g.DisplayModel,
					Position:     i + 1,
					TotalEntries: len(g.PriorityOrder),
				}
				writeJSON(w, resp)
				return
			}
		}
	}

	http.Error(w, "model not in any failover group", http.StatusNotFound)
}

func (h *FailoverHandler) buildGroupResponse(ctx context.Context, g *failover.FailoverGroup) (FailoverGroupResponse, error) {
	models, err := h.modelRepo.GetByIDs(ctx, g.PriorityOrder)
	if err != nil {
		return FailoverGroupResponse{}, err
	}

	entries := make([]FailoverEntryResponse, 0, len(g.PriorityOrder))
	for _, modelUUID := range g.PriorityOrder {
		m, ok := models[modelUUID]
		if !ok {
			continue
		}

		enabled := true
		if val, ok := g.EntryEnabled[modelUUID.String()]; ok {
			enabled = val
		}

		entries = append(entries, FailoverEntryResponse{
			ModelUUID:     modelUUID.String(),
			ModelID:       m.ModelID,
			ProviderID:    m.ProviderID.String(),
			ProviderName:  m.ProviderName,
			DisplayName:   m.DisplayName,
			Enabled:       enabled,
			ContextLength: m.ContextLength,
			OwnedBy:       m.OwnedBy,
		})
	}

	var createdAt, updatedAt string
	if !g.CreatedAt.IsZero() {
		createdAt = g.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if !g.UpdatedAt.IsZero() {
		updatedAt = g.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}

	return FailoverGroupResponse{
		ID:           g.ID.String(),
		DisplayModel: g.DisplayModel,
		DisplayName:  g.DisplayName,
		Description:  g.Description,
		GroupEnabled: g.GroupEnabled,
		AutoCreated:  g.AutoCreated,
		Entries:      entries,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}
