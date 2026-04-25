package proxy

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/ratelimit"
	"github.com/user/llm-proxy/internal/settings"
	"github.com/user/llm-proxy/internal/util"
	"github.com/user/llm-proxy/internal/virtualkey"
)

type Handler struct {
	cfg            *config.Config
	providerRepo   *provider.Repository
	modelRepo      *model.Repository
	dbPool         *pgxpool.Pool
	virtualKeyRepo *virtualkey.Repository
	failoverRepo   *failover.Repository
	settingsRepo   *settings.Repository
	rateLimiter    *ratelimit.Limiter
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
) *Handler {
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		dbPool:         dbPool,
		virtualKeyRepo: virtualKeyRepo,
		failoverRepo:   failoverRepo,
		settingsRepo:   settingsRepo,
		rateLimiter:    rateLimiter,
	}
}

func (h *Handler) Register(r chi.Router) {
	r.Use(h.ProxyKeyMiddleware)
	r.Use(h.rateLimiter.Middleware(h.cfg.RateLimitEnabled))

	r.Get("/models", h.ListModels)
	r.Post("/chat/completions", h.ChatCompletions)
}

func (h *Handler) RegisterAdminChat(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), virtualKeyNameKey, "chat")
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
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		keyHash := virtualkey.Hash(token)
		vk, err := h.virtualKeyRepo.FindByKeyHash(r.Context(), keyHash)
		if err != nil {
			http.Error(w, "Invalid virtual key", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), virtualKeyNameKey, vk.Name)
		ctx = context.WithValue(ctx, virtualKeyIDKey, vk.ID.String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
		h.virtualKeyRepo.TouchLastUsed(context.Background(), keyHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
