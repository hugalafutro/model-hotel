package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
)

type FailoverHandler struct {
	failoverRepo *failover.Repository
	modelRepo    *model.Repository
}

func NewFailoverHandler(failoverRepo *failover.Repository, modelRepo *model.Repository) *FailoverHandler {
	return &FailoverHandler{
		failoverRepo: failoverRepo,
		modelRepo:    modelRepo,
	}
}

type FailoverEntryResponse struct {
	ModelUUID     string  `json:"model_uuid"`
	ModelID       string  `json:"model_id"`
	ProviderID    string  `json:"provider_id"`
	ProviderName  string  `json:"provider_name"`
	DisplayName   string  `json:"display_name"`
	Enabled       bool    `json:"enabled"`
	ContextLength *int    `json:"context_length"`
	OwnedBy       string  `json:"owned_by"`
}

type FailoverGroupResponse struct {
	ID           string                `json:"id"`
	DisplayModel string                `json:"display_model"`
	DisplayName  *string               `json:"display_name"`
	Description  string                `json:"description"`
	GroupEnabled bool                  `json:"group_enabled"`
	AutoCreated  bool                  `json:"auto_created"`
	Entries      []FailoverEntryResponse `json:"entries"`
	CreatedAt    string                `json:"created_at"`
	UpdatedAt    string                `json:"updated_at"`
}

func (h *FailoverHandler) Register(r chi.Router) {
	r.Route("/failover-groups", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/sync", h.Sync)
		r.Get("/candidates", h.Candidates)
		r.Get("/{id}", h.Get)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

func (h *FailoverHandler) List(w http.ResponseWriter, r *http.Request) {
	groups, err := h.failoverRepo.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]FailoverGroupResponse, len(groups))
	for i, g := range groups {
		resp, err := h.buildGroupResponse(r.Context(), g)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		responses[i] = resp
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

func (h *FailoverHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid failover group ID", http.StatusBadRequest)
		return
	}

	g, err := h.failoverRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "failover group not found", http.StatusNotFound)
		return
	}

	resp, err := h.buildGroupResponse(r.Context(), g)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type CreateFailoverGroupRequest struct {
	DisplayModel string   `json:"display_model"`
	DisplayName  *string  `json:"display_name"`
	Description  *string  `json:"description"`
	EntryIDs     []string `json:"entry_ids"`
}

func (h *FailoverHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateFailoverGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.DisplayModel == "" {
		http.Error(w, "display_model is required", http.StatusBadRequest)
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

	autoCreated := false
	group, err := h.failoverRepo.UpsertWithConfig(r.Context(), req.DisplayModel, priorityOrder, 
		entryEnabled, nil, req.DisplayName, req.Description, &autoCreated)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := h.buildGroupResponse(r.Context(), group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

type UpdateFailoverGroupRequest struct {
	DisplayName   *string             `json:"display_name"`
	Description   *string             `json:"description"`
	GroupEnabled  *bool               `json:"group_enabled"`
	PriorityOrder []string            `json:"priority_order"`
	EntryEnabled  map[string]bool     `json:"entry_enabled"`
}

func (h *FailoverHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid failover group ID", http.StatusBadRequest)
		return
	}

	var req UpdateFailoverGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	existing, err := h.failoverRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "failover group not found", http.StatusNotFound)
		return
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

	group, err := h.failoverRepo.Update(r.Context(), id, priorityOrder, entryEnabled, 
		req.GroupEnabled, req.DisplayName, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := h.buildGroupResponse(r.Context(), group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *FailoverHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid failover group ID", http.StatusBadRequest)
		return
	}

	if err := h.failoverRepo.DeleteByID(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *FailoverHandler) Sync(w http.ResponseWriter, r *http.Request) {
	if err := h.failoverRepo.SyncAllModels(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type CandidateModelResponse struct {
	ModelUUID     string `json:"model_uuid"`
	ModelID       string `json:"model_id"`
	ProviderID    string `json:"provider_id"`
	ProviderName  string `json:"provider_name"`
	DisplayName   string `json:"display_name"`
	ContextLength *int   `json:"context_length"`
	OwnedBy       string `json:"owned_by"`
}

func (h *FailoverHandler) Candidates(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelRepo.List(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(candidates)
}

func (h *FailoverHandler) buildGroupResponse(ctx context.Context, g *failover.FailoverGroup) (FailoverGroupResponse, error) {
	entries := make([]FailoverEntryResponse, 0, len(g.PriorityOrder))
	for _, modelUUID := range g.PriorityOrder {
		m, err := h.modelRepo.Get(ctx, modelUUID)
		if err != nil {
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