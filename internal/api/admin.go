package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/user/llm-proxy/internal/admin"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/db"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/settings"
	"github.com/user/llm-proxy/internal/util"
	"github.com/user/llm-proxy/internal/virtualkey"
)

type Handler struct {
	cfg            *config.Config
	providerRepo   *provider.Repository
	dbPool         *db.DB
	adminMgr       *admin.Manager
	virtualKeyRepo *virtualkey.Repository
	settingsRepo   *settings.Repository
}

func NewHandler(cfg *config.Config, providerRepo *provider.Repository, database *db.DB, adminMgr *admin.Manager, vkRepo *virtualkey.Repository, settingsRepo *settings.Repository) *Handler {
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		dbPool:         database,
		adminMgr:       adminMgr,
		virtualKeyRepo: vkRepo,
		settingsRepo:   settingsRepo,
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

	h.RegisterModels(r)
	h.RegisterProviderDiscovery(r)
	h.RegisterLogs(r)
	h.RegisterSettings(r)
	h.RegisterVirtualKeys(r)

	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	modelRepo := model.NewRepository(h.dbPool.Pool())
	NewFailoverHandler(h.dbPool.Pool(), failoverRepo, modelRepo, h.settingsRepo).Register(r)

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

// RegisterEvents registers the SSE endpoint on a route group that is
// exempt from the chi Timeout middleware.  SSE connections are long-lived
// and must not be killed by a 60-second request deadline; the handler
// detects client disconnect via r.Context().Done() instead.
func (h *Handler) RegisterEvents(r chi.Router) {
	r.Use(h.AuthMiddleware)
	r.Get("/events", h.StreamEvents)
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

	if !h.cfg.AllowHTTPProviders {
		parsed, err := url.Parse(strings.TrimSpace(req.BaseURL))
		if err != nil || parsed.Scheme != "https" {
			http.Error(w, "base_url must use HTTPS (set ALLOW_HTTP_PROVIDERS=true for HTTP)", http.StatusBadRequest)
			return
		}
	}

	if err := h.cfg.ValidateProviderURL(req.BaseURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Application-level duplicate name check
	existing, _ := h.providerRepo.GetByName(r.Context(), req.Name)
	if existing != nil {
		http.Error(w, "a provider with this name already exists", http.StatusConflict)
		return
	}

	encryptedKey, err := auth.Encrypt(req.APIKey, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to encrypt API key", http.StatusInternalServerError)
		return
	}

	p, err := h.providerRepo.Create(r.Context(), req, encryptedKey.Ciphertext, encryptedKey.Nonce, encryptedKey.Salt)
	if err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.cfg.MasterKey)

	response := provider.ToResponse(p)
	writeJSONCreated(w, response)
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

	tokenRows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, COALESCE(SUM(tokens_prompt + tokens_completion), 0) FROM request_logs WHERE provider_id IS NOT NULL GROUP BY provider_id")
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

	writeJSON(w, responses)
}

func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	p, err := h.providerRepo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)

	var modelCount int
	if err := h.dbPool.Pool().QueryRow(r.Context(), "SELECT COUNT(*) FROM models WHERE provider_id = $1", p.ID).Scan(&modelCount); err == nil {
		response.ModelCount = modelCount
	}

	writeJSON(w, response)
}

func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	var req provider.UpdateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Application-level duplicate name check when renaming
	if req.Name != nil {
		existing, _ := h.providerRepo.GetByName(r.Context(), *req.Name)
		if existing != nil && existing.ID != id {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
	}

	// Validate new BaseURL if provided
	if req.BaseURL != nil {
		if !h.cfg.AllowHTTPProviders {
			parsed, err := url.Parse(strings.TrimSpace(*req.BaseURL))
			if err != nil || parsed.Scheme != "https" {
				http.Error(w, "base_url must use HTTPS (set ALLOW_HTTP_PROVIDERS=true for HTTP)", http.StatusBadRequest)
				return
			}
		}
		if err := h.cfg.ValidateProviderURL(*req.BaseURL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
		if isUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)
	writeJSON(w, response)
}

func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	if err := h.providerRepo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// isUniqueViolation checks if the error is a PostgreSQL unique constraint violation (error code 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
