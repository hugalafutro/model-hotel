package proxy

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

type Handler struct {
	cfg            *config.Config
	providerRepo   *provider.Repository
	modelRepo      *model.Repository
	dbPool         *pgxpool.Pool
	virtualKeyRepo VirtualKeyRepository
	failoverRepo   *failover.Repository
	settingsRepo   *settings.Repository
	rateLimiter    *ratelimit.Limiter
	ipLimiter      *ratelimit.IPLimiter
	circuitBreaker *failover.CircuitBreaker
	// upstreamTransport is a shared Transport for all outbound proxy
	// requests.  Reusing one Transport avoids creating a fresh Transport
	// (and its persistent readLoop/writeLoop goroutines) per request.
	upstreamTransport *http.Transport

	// deprecationCache caches rejected parameters learned from HTTP 400 responses,
	// keyed by "providerType:modelID". Value: map[string]bool of rejected param names.
	deprecationCache sync.Map
}

// virtualKeyRepoAdapter wraps *virtualkey.Repository to implement VirtualKeyRepository.
type virtualKeyRepoAdapter struct {
	repo *virtualkey.Repository
}

func (a *virtualKeyRepoAdapter) AddTokens(ctx context.Context, keyHash string, tokens int) error {
	return a.repo.AddTokens(ctx, keyHash, tokens)
}

func (a *virtualKeyRepoAdapter) TouchLastUsed(ctx context.Context, keyHash string) error {
	return a.repo.TouchLastUsed(ctx, keyHash)
}

func (a *virtualKeyRepoAdapter) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
	vk, err := a.repo.FindByKeyHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}
	return &VirtualKeyInfo{
		ID:             vk.ID.String(),
		Name:           vk.Name,
		KeyHash:        vk.KeyHash,
		KeyPreview:     vk.KeyPreview,
		TokensUsed:     vk.TokensUsed,
		RateLimitRPS:   vk.RateLimitRPS,
		RateLimitBurst: vk.RateLimitBurst,
	}, nil
}

func (a *virtualKeyRepoAdapter) Create(ctx context.Context, name, keyHash, keyPreview string) (*VirtualKeyInfo, error) {
	vk, err := a.repo.Create(ctx, name, keyHash, keyPreview, nil, nil)
	if err != nil {
		return nil, err
	}
	return &VirtualKeyInfo{
		ID:             vk.ID.String(),
		Name:           vk.Name,
		KeyHash:        vk.KeyHash,
		KeyPreview:     vk.KeyPreview,
		TokensUsed:     vk.TokensUsed,
		RateLimitRPS:   vk.RateLimitRPS,
		RateLimitBurst: vk.RateLimitBurst,
	}, nil
}

func (a *virtualKeyRepoAdapter) Delete(ctx context.Context, id string) error {
	vid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	return a.repo.Delete(ctx, vid)
}

func NewHandler(
	cfg *config.Config,
	providerRepo *provider.Repository,
	modelRepo *model.Repository,
	dbPool *pgxpool.Pool,
	virtualKeyRepo *virtualkey.Repository,
	failoverRepo *failover.Repository,
	settingsRepo *settings.Repository,
	rateLimiter *ratelimit.Limiter,
	ipLimiter *ratelimit.IPLimiter,
) *Handler {
	sd := NewSafeDialer(append(cfg.AllowedProviderHosts, config.KnownProviderHosts()...))
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		dbPool:         dbPool,
		virtualKeyRepo: &virtualKeyRepoAdapter{repo: virtualKeyRepo},
		failoverRepo:   failoverRepo,
		settingsRepo:   settingsRepo,
		rateLimiter:    rateLimiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: &http.Transport{
			DialContext:           sd.DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

// Close releases resources owned by the handler. Call during server
// shutdown so the shared upstream Transport terminates its idle
// connection goroutines.
func (h *Handler) Close() {
	if h.upstreamTransport != nil {
		h.upstreamTransport.CloseIdleConnections()
		debuglog.Info("proxy: closed upstream transport")
	}
}

func (h *Handler) Register(r chi.Router) {
	r.Use(h.ipLimiter.Middleware)
	r.Use(h.ProxyKeyMiddleware)
	r.Use(h.rateLimiter.Middleware(h.cfg.RateLimitEnabled))

	r.Get("/models", h.ListModels)
	r.Post("/chat/completions", h.ChatCompletions)
}

func (h *Handler) RegisterAdminChat(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := "chat"
			switch r.URL.Path {
			case "/api/chat/arena":
				key = "arena"
			case "/api/chat/completions":
				key = "completions"
			}
			debuglog.Debug("admin-chat: routing request", "path", r.URL.Path, "key", key)
			ctx := context.WithValue(r.Context(), virtualKeyNameKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(h.rateLimiter.Middleware(h.cfg.RateLimitEnabled))

	r.Post("/chat", h.ChatCompletions)
	r.Post("/arena", h.ChatCompletions)
	r.Post("/completions", h.ChatCompletions)
}

func (h *Handler) ProxyKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := util.ParseBearerToken(r)
		if !ok {
			debuglog.Error("auth: missing authorization header", "remote_addr", r.RemoteAddr)
			writeOpenAIError(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		keyHash := virtualkey.Hash(token)
		vk, err := h.virtualKeyRepo.FindByKeyHash(r.Context(), keyHash)
		if err != nil {
			if errors.Is(err, virtualkey.ErrNotFound) {
				debuglog.Error("auth: key not found", "remote_addr", r.RemoteAddr)
				writeOpenAIError(w, "Invalid virtual key", http.StatusUnauthorized)
			} else {
				debuglog.Error("auth: db lookup failed", "error", err)
				writeOpenAIError(w, "Internal error", http.StatusInternalServerError)
			}
			return
		}
		debuglog.Info("auth: authenticated", "key", vk.Name)
		ctx := context.WithValue(r.Context(), virtualKeyNameKey, vk.Name)
		ctx = context.WithValue(ctx, virtualKeyIDKey, vk.ID)
		ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitRPSKey, vk.RateLimitRPS)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitBurstKey, vk.RateLimitBurst)
		// Fire-and-forget touch with a timeout so the goroutine cannot
		// outlive the server if the DB is slow.
		go func(hash string) {
			defer func() {
				if r := recover(); r != nil {
					debuglog.Error("proxy: panic in TouchLastUsed (virtual key)", "error", r)
				}
			}()
			tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer tcancel()
			if err := h.virtualKeyRepo.TouchLastUsed(tctx, hash); err != nil {
				debuglog.Debug("proxy: failed to touch virtual key last-used", "error", err)
			}
		}(keyHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
