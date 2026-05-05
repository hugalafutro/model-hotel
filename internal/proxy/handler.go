package proxy

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/jackc/pgx/v5/pgxpool"
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
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		dbPool:         dbPool,
		virtualKeyRepo: virtualKeyRepo,
		failoverRepo:   failoverRepo,
		settingsRepo:   settingsRepo,
		rateLimiter:    rateLimiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
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
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[proxy] panic in TouchLastUsed (virtual key): %v", r)
				}
			}()
			tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer tcancel()
			_ = h.virtualKeyRepo.TouchLastUsed(tctx, hash)
		}(keyHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
