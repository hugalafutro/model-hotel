package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/admin"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/db"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/util"
	"github.com/user/llm-proxy/internal/virtualkey"
)

type Handler struct {
	cfg            *config.Config
	providerRepo   *provider.Repository
	dbPool         *db.DB
	adminMgr       *admin.Manager
	virtualKeyRepo *virtualkey.Repository
}

func NewHandler(cfg *config.Config, providerRepo *provider.Repository, database *db.DB, adminMgr *admin.Manager, vkRepo *virtualkey.Repository) *Handler {
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		dbPool:         database,
		adminMgr:       adminMgr,
		virtualKeyRepo: vkRepo,
	}
}

func (h *Handler) Pool() *db.DB {
	return h.dbPool
}

func (h *Handler) Register(r chi.Router) {
	r.Use(h.AuthMiddleware)

	r.Route("/providers", func(r chi.Router) {
		r.Post("/", h.CreateProvider)
		r.Get("/", h.ListProviders)
		r.Get("/{id}", h.GetProvider)
		r.Put("/{id}", h.UpdateProvider)
		r.Delete("/{id}", h.DeleteProvider)
	})

	r.Route("/keys", func(r chi.Router) {
		r.Post("/", h.CreateProxyKey)
		r.Get("/", h.ListProxyKeys)
		r.Delete("/{id}", h.RevokeProxyKey)
	})

	h.RegisterModels(r)
	h.RegisterProviderDiscovery(r)
	h.RegisterLogs(r)
	h.RegisterSettings(r)
	h.RegisterVirtualKeys(r)

	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	modelRepo := model.NewRepository(h.dbPool.Pool())
	NewFailoverHandler(h.dbPool.Pool(), failoverRepo, modelRepo).Register(r)

	NewStatsHandler(h.dbPool.Pool(), h.adminMgr).Register(r)
	NewSystemHandler(h.dbPool.Pool()).Register(r)
}

func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := util.ParseBearerToken(r)
		if !ok {
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		if !h.adminMgr.Validate(token) {
			http.Error(w, "Invalid admin token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req provider.CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if len(req.Name) > 100 {
		http.Error(w, "name must be less than 100 characters", http.StatusBadRequest)
		return
	}

	if req.BaseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}

	if len(req.BaseURL) > 500 {
		http.Error(w, "base_url must be less than 500 characters", http.StatusBadRequest)
		return
	}

	if req.APIKey == "" {
		http.Error(w, "api_key is required", http.StatusBadRequest)
		return
	}

	if len(req.APIKey) > 500 {
		http.Error(w, "api_key must be less than 500 characters", http.StatusBadRequest)
		return
	}

	if !h.cfg.AllowHTTPProviders && len(req.BaseURL) >= 8 && req.BaseURL[:8] != "https://" {
		http.Error(w, "base_url must use HTTPS (set ALLOW_HTTP_PROVIDERS=true for HTTP)", http.StatusBadRequest)
		return
	}

	encryptedKey, err := auth.Encrypt(req.APIKey, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to encrypt API key", http.StatusInternalServerError)
		return
	}

	p, err := h.providerRepo.Create(r.Context(), req, encryptedKey.Ciphertext, encryptedKey.Nonce, encryptedKey.Salt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.cfg.MasterKey)

	response := provider.ToResponse(p)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, COUNT(*) FROM models GROUP BY provider_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	modelCounts := make(map[string]int)
	for rows.Next() {
		var providerID string
		var count int
		if err := rows.Scan(&providerID, &count); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		modelCounts[providerID] = count
	}

	tokenRows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, COALESCE(SUM(tokens_prompt + tokens_completion), 0) FROM request_logs GROUP BY provider_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tokenRows.Close()

	tokenCounts := make(map[string]int)
	for tokenRows.Next() {
		var providerID string
		var total int
		if err := tokenRows.Scan(&providerID, &total); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tokenCounts[providerID] = total
	}

	responses := make([]provider.ProviderResponse, len(providers))
	for i, p := range providers {
		responses[i] = provider.ToResponse(p)
		responses[i].ModelCount = modelCounts[p.ID.String()]
		responses[i].TotalTokens = tokenCounts[p.ID.String()]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}

	p, err := h.providerRepo.Get(r.Context(), id)
	if err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}

	var req provider.UpdateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var encryptedKey []byte
	var keyNonce []byte
	var keySalt []byte

	if req.APIKey != nil {
		enc, err := auth.Encrypt(*req.APIKey, h.cfg.MasterKey)
		if err != nil {
			http.Error(w, "failed to encrypt API key", http.StatusInternalServerError)
			return
		}
		encryptedKey = enc.Ciphertext
		keyNonce = enc.Nonce
		keySalt = enc.Salt
	}

	p, err := h.providerRepo.Update(r.Context(), id, req, encryptedKey, keyNonce, keySalt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}

	if err := h.providerRepo.Delete(r.Context(), id); err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
