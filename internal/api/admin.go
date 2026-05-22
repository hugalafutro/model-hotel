// Package api provides HTTP handlers and routing for the admin API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ProviderStore defines the provider repository methods used by the API.
type ProviderStore interface {
	Create(ctx context.Context, req provider.CreateProviderRequest, encryptedKey, keyNonce, keySalt []byte) (*provider.Provider, error)
	List(ctx context.Context) ([]*provider.Provider, error)
	Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error)
	GetByName(ctx context.Context, name string) (*provider.Provider, error)
	Update(ctx context.Context, id uuid.UUID, req provider.UpdateProviderRequest, encryptedKey, keyNonce, keySalt []byte) (*provider.Provider, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// VirtualKeyStore defines the virtual key repository methods used by the API.
type VirtualKeyStore interface {
	Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*virtualkey.VirtualKey, error)
	List(ctx context.Context) ([]*virtualkey.VirtualKey, error)
	Get(ctx context.Context, id uuid.UUID) (*virtualkey.VirtualKey, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Update(ctx context.Context, id uuid.UUID, name string, rps *float64, burst *int) (*virtualkey.VirtualKey, error)
}

// SettingsStore defines the settings repository methods used by the API.
type SettingsStore interface {
	GetAll(ctx context.Context) (map[string]string, error)
	GetWithDefault(ctx context.Context, key string, defaultValue string) string
	Set(ctx context.Context, key string, value string) error
	SetTx(ctx context.Context, tx pgx.Tx, key string, value string) error
	InvalidateCache(key string)
}

// AdminAuthenticator defines admin token validation.
type AdminAuthenticator interface {
	Validate(token string) bool
}

// Handler manages admin API operations for providers, models, and virtual keys.
type Handler struct {
	cfg            *config.Config
	providerRepo   ProviderStore
	dbPool         *db.DB
	adminMgr       AdminAuthenticator
	virtualKeyRepo VirtualKeyStore
	settingsRepo   SettingsStore
	systemHandler  *SystemHandler
	appVersion     string
	ghReleasesURL  string // injectable for testing; defaults to githubReleasesURL const
}

// NewHandler creates a new admin API handler with the given dependencies.
func NewHandler(cfg *config.Config, providerRepo ProviderStore, database *db.DB, adminMgr AdminAuthenticator, vkRepo VirtualKeyStore, settingsRepo SettingsStore, appVersion string) *Handler {
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		dbPool:         database,
		adminMgr:       adminMgr,
		virtualKeyRepo: vkRepo,
		settingsRepo:   settingsRepo,
		appVersion:     appVersion,
		ghReleasesURL:  githubReleasesURL,
	}
}

// Pool returns the database connection pool.
func (h *Handler) Pool() *db.DB {
	return h.dbPool
}

// SetDockerStatsCollector overrides the system Docker stats collector (for testing).
func (h *Handler) SetDockerStatsCollector(fn dockerStatsCollector) {
	if h.systemHandler != nil {
		h.systemHandler.dockerStatsCollector = fn
	}
}

// Register mounts all admin API routes on the given router.
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
	h.RegisterAppLogs(r)
	h.RegisterSettings(r)
	h.RegisterVirtualKeys(r)
	h.RegisterVersion(r)

	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	modelRepo := model.NewRepository(h.dbPool.Pool())
	NewFailoverHandler(h.dbPool.Pool(), failoverRepo, modelRepo, h.settingsRepo).Register(r)

	NewStatsHandler(h.dbPool.Pool(), h.adminMgr).Register(r)
	sh := NewSystemHandler(h.dbPool.Pool())
	sh.Register(r)
	h.systemHandler = sh
	NewBackupHandler(h.cfg.DatabaseURL, filepath.Join(h.cfg.DataDir, "backups"), h.adminMgr).Register(r)
}

// AuthMiddleware validates admin token authentication for all admin API requests.
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

// CreateProvider creates a new provider.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req provider.CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	trimmed, err := validateNameString("name", req.Name, 1, 100)
	if err != nil {
		respondBadRequest(w, "invalid name", err)
		return
	}
	req.Name = trimmed

	if req.BaseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}

	if len(req.BaseURL) > 500 {
		http.Error(w, "base_url must be less than 500 characters", http.StatusBadRequest)
		return
	}

	// Some providers (e.g. OpenCode Zen) support keyless access for free models.
	// Allow empty API key only for providers that support it.
	if req.APIKey == "" && !providerTypeAllowsEmptyKey(req.BaseURL) {
		http.Error(w, "api_key is required for this provider type", http.StatusBadRequest)
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
		respondBadRequest(w, "invalid provider URL", err)
		return
	}

	// Application-level duplicate name check
	existing, _ := h.providerRepo.GetByName(r.Context(), req.Name)
	if existing != nil {
		http.Error(w, "a provider with this name already exists", http.StatusConflict)
		return
	}

	var encryptedKey *auth.KeyPair
	if req.APIKey != "" {
		var encErr error
		encryptedKey, encErr = auth.Encrypt(req.APIKey, h.cfg.MasterKey)
		if encErr != nil {
			respondError(w, fmt.Sprintf("failed to encrypt API key for provider %q", req.Name), encErr, http.StatusInternalServerError)
			return
		}
	}

	var encCiphertext, encNonce, encSalt []byte
	if encryptedKey != nil {
		encCiphertext = encryptedKey.Ciphertext
		encNonce = encryptedKey.Nonce
		encSalt = encryptedKey.Salt
	}

	p, err := h.providerRepo.Create(r.Context(), req, encCiphertext, encNonce, encSalt)
	if err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		respondError(w, fmt.Sprintf("failed to create provider %q", req.Name), err, http.StatusInternalServerError)
		return
	}

	// Skip key cache warming for keyless providers (nil encrypted key bytes)
	if len(p.EncryptedKey) > 0 {
		go auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.cfg.MasterKey)
	}

	response := provider.ToResponse(p)
	writeJSONCreated(w, response)
}

// ListProviders returns all configured providers.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", err, http.StatusInternalServerError)
		return
	}

	rows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, COUNT(*) FROM models GROUP BY provider_id")
	if err != nil {
		respondError(w, "failed to query model counts", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	modelCounts := make(map[string]int)
	for rows.Next() {
		var providerID string
		var count int
		if err := rows.Scan(&providerID, &count); err != nil {
			respondError(w, "failed to scan model count row", err, http.StatusInternalServerError)
			return
		}
		modelCounts[providerID] = count
	}

	tokenRows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, SUM(COALESCE(tokens_prompt, 0) + COALESCE(tokens_completion, 0)) FROM request_logs WHERE provider_id IS NOT NULL GROUP BY provider_id")
	if err != nil {
		respondError(w, "failed to query token counts", err, http.StatusInternalServerError)
		return
	}
	defer tokenRows.Close()

	tokenCounts := make(map[string]int)
	for tokenRows.Next() {
		var providerID string
		var total int
		if err := tokenRows.Scan(&providerID, &total); err != nil {
			respondError(w, "failed to scan token count row", err, http.StatusInternalServerError)
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

// GetProvider returns a single provider by ID.
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
		respondError(w, fmt.Sprintf("failed to get provider %s", id), err, http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)

	var modelCount int
	if err := h.dbPool.Pool().QueryRow(r.Context(), "SELECT COUNT(*) FROM models WHERE provider_id = $1", p.ID).Scan(&modelCount); err == nil {
		response.ModelCount = modelCount
	}

	writeJSON(w, response)
}

// UpdateProvider updates an existing provider by ID.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	var req provider.UpdateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	// Validate field lengths
	if req.Name != nil {
		trimmed, err := validateNamePtr("name", req.Name, 1, 100)
		if err != nil {
			respondBadRequest(w, "invalid name", err)
			return
		}
		req.Name = trimmed
	}

	if req.BaseURL != nil {
		trimmed := trimString(*req.BaseURL)
		req.BaseURL = &trimmed
		if err := validateStringPtrLength("base_url", req.BaseURL, 1, 500); err != nil {
			respondBadRequest(w, "invalid base URL", err)
			return
		}
	}

	if req.APIKey != nil {
		if len(*req.APIKey) > 500 {
			http.Error(w, "api_key must be at most 500 characters", http.StatusBadRequest)
			return
		}
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
			respondBadRequest(w, "invalid provider URL", err)
			return
		}
	}

	var encryptedKey []byte
	var keyNonce []byte
	var keySalt []byte

	if req.APIKey != nil {
		enc, encErr := auth.Encrypt(*req.APIKey, h.cfg.MasterKey)
		if encErr != nil {
			respondError(w, "failed to encrypt API key", encErr, http.StatusInternalServerError)
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
		respondError(w, fmt.Sprintf("failed to update provider %s", id), err, http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)
	writeJSON(w, response)
}

// DeleteProvider removes a provider by ID and cleans up associated data.
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
		respondError(w, fmt.Sprintf("failed to delete provider %s", id), err, http.StatusInternalServerError)
		return
	}

	// Sync failover groups since the cascade-deleted models may leave
	// groups with stale entries or zero candidates.
	// Guarded because unit tests pass nil dbPool.
	if h.dbPool != nil {
		failoverRepo := failover.NewRepository(h.dbPool.Pool())
		if _, err := failoverRepo.SyncAllModels(context.WithoutCancel(r.Context())); err != nil {
			// Log but don't fail the delete — the provider is already gone.
			debuglog.Info("admin: failed to sync failover groups after provider delete", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// providerTypeAllowsEmptyKey returns true for provider types that support keyless
// access (e.g. OpenCode Zen, Ollama, which allow free models without an API key).
func providerTypeAllowsEmptyKey(baseURL string) bool {
	providerType := provider.DetectProviderType(baseURL)
	switch providerType {
	case "opencode-zen", "ollama", "koboldcpp", "lmstudio", "custom":
		return true
	default:
		return false
	}
}

// isUniqueViolation checks if the error is a PostgreSQL unique constraint violation (error code 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
