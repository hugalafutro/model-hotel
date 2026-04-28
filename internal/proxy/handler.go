package proxy

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

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
	// upstreamTransport is a shared Transport for all outbound proxy
	// requests.  Reusing one Transport avoids creating a fresh Transport
	// (and its persistent readLoop/writeLoop goroutines) per request.
	upstreamTransport *http.Transport
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
		upstreamTransport: &http.Transport{
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
		log.Printf("[proxy] closed upstream transport")
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
			key := "chat"
			switch r.URL.Path {
			case "/api/chat/arena":
				key = "arena"
			case "/api/chat/completions":
				key = "completions"
			}
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
			log.Printf("[auth] error: missing authorization header from %s", r.RemoteAddr)
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		keyHash := virtualkey.Hash(token)
		vk, err := h.virtualKeyRepo.FindByKeyHash(r.Context(), keyHash)
		if err != nil {
			if errors.Is(err, virtualkey.ErrNotFound) {
				log.Printf("[auth] error: key not found from %s", r.RemoteAddr)
				http.Error(w, "Invalid virtual key", http.StatusUnauthorized)
			} else {
				log.Printf("[auth] error: db lookup failed: %v", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
			}
			return
		}
		log.Printf("[auth] authenticated key=%q", vk.Name)
		ctx := context.WithValue(r.Context(), virtualKeyNameKey, vk.Name)
		ctx = context.WithValue(ctx, virtualKeyIDKey, vk.ID.String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
		// Fire-and-forget touch with a timeout so the goroutine cannot
		// outlive the server if the DB is slow.
		go func(hash string) {
			tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer tcancel()
			h.virtualKeyRepo.TouchLastUsed(tctx, hash)
		}(keyHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
