package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// Handler manages proxy routes and middleware.
type Handler struct {
	cfg            *config.Config
	providerRepo   *provider.Repository
	modelRepo      ModelRepository
	dbPool         *pgxpool.Pool
	virtualKeyRepo VirtualKeyRepository
	failoverRepo   *failover.Repository
	settingsRepo   *settings.Repository
	rateLimiter    *ratelimit.Limiter
	tpmLimiter     *ratelimit.TPMLimiter
	ipLimiter      *ratelimit.IPLimiter
	circuitBreaker *failover.CircuitBreaker
	// capLedger remembers each provider's last exhausted 429 for the quota
	// badge of a provider with no usage API (provider.CapLedger). Created on
	// first use, so a Handler built as a literal has one too.
	capLedger     *provider.CapLedger
	capLedgerOnce sync.Once
	// inflight is the adaptive per-provider concurrency learner (see
	// inflight.go). Nil (tests building Handler{} directly) admits everything
	// and learns nothing. NewHandler registers its scrape-time gauges; the
	// registration is once-guarded, so like the breaker collector it reports
	// the first handler's state — one handler exists outside tests.
	inflight *inflightLimiter
	// upstreamTransport is a shared Transport for all outbound proxy
	// requests.  Reusing one Transport avoids creating a fresh Transport
	// (and its persistent readLoop/writeLoop goroutines) per request.
	upstreamTransport *http.Transport
	// shutdown is closed by Close so in-flight streams can end with a
	// well-formed error frame instead of a cut connection when the process
	// is stopping; nil (tests building Handler{} directly) never fires.
	shutdown     chan struct{}
	shutdownOnce sync.Once

	// safeDialer holds the SafeDialer for use in CheckRedirect.
	safeDialer *SafeDialer

	// deprecationCache caches rejected parameters learned from HTTP 400 responses,
	// keyed by "providerType:modelID". Value: map[string]bool of rejected param names.
	deprecationCache sync.Map
	// paramRenameCache caches param renames learned from HTTP 400 responses (params
	// the upstream wants renamed rather than dropped, e.g. max_tokens ->
	// max_completion_tokens for OpenAI gpt-5/o-series), keyed by
	// "providerType:modelID". Value: map[string]string of old->new param names.
	paramRenameCache sync.Map
	// thinkingDialectCache caches which extended-thinking shape a model wants,
	// learned from an upstream 400, keyed by the same "providerScope:modelID" the
	// param caches use. Value: anthropicegress.ThinkingDialect.
	//
	// Only models that answered a 400 appear here; everything else takes the
	// adaptive default. In-memory and per-instance on purpose, like the param
	// caches: the cost of relearning after a restart is one 400 on the first
	// thinking request to a budget-dialect model, and the alternative is a
	// persisted fact that goes stale the next time Anthropic moves a model
	// between dialects.
	thinkingDialectCache sync.Map
	// goneStrikes counts consecutive KindProviderModelGone responses per model
	// UUID, so a model the provider has retired is probed and then disabled
	// after goneStrikeThreshold refusals. Deliberately in-memory and
	// per-instance; see noteModelGone for why it is not persisted.
	//
	// Value: *goneStreak, not a plain int. A retired model is precisely the one
	// taking concurrent refusals, so the increment has to be atomic or racing
	// strikes overwrite each other and the streak never lands. The struct also
	// carries the time of the last strike, which bounds how far apart the
	// refusals in one streak may be, and the tombstone a success uses to stand
	// down a disable that has been decided but not yet written.
	goneStrikes sync.Map
	// goneProbeSlots bounds how many pre-retirement probes may be in flight
	// against one provider at once, keyed by provider UUID. Value: a
	// chan struct{} of goneProbeMaxConcurrent capacity, used as a non-blocking
	// semaphore (see acquireProbeSlot). Per gateway instance, like goneStrikes
	// beside it: the cap describes what THIS gateway will aim at a provider.
	goneProbeSlots sync.Map
	// responsesRequiredCache remembers models whose upstream 400'd
	// tools+reasoning over chat-completions and demanded /v1/responses (OpenAI
	// gpt-5.4+/gpt-5.6 families), keyed by "providerType:modelID". Once a model
	// is flagged, matching requests route to /v1/responses preemptively.
	responsesRequiredCache sync.Map
	// waitInsertTimeout overrides the default 5s WaitForInsert timeout.
	// Zero means use the default. Set by tests only.
	waitInsertTimeout time.Duration
}

// virtualKeyRepoAdapter wraps *virtualkey.Repository to implement VirtualKeyRepository.
type virtualKeyRepoAdapter struct {
	repo *virtualkey.Repository
}

// WrapVirtualKeyRepo wraps a *virtualkey.Repository to implement the VirtualKeyRepository interface.
// This is needed because the proxy package uses VirtualKeyInfo (a subset of virtualkey.VirtualKey)
// and the interface signatures may diverge from the concrete repository.
func WrapVirtualKeyRepo(repo *virtualkey.Repository) VirtualKeyRepository {
	return &virtualKeyRepoAdapter{repo: repo}
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
	info := &VirtualKeyInfo{
		ID:               vk.ID.String(),
		Name:             vk.Name,
		KeyHash:          vk.KeyHash,
		KeyPreview:       vk.KeyPreview,
		TokensUsed:       vk.TokensUsed,
		RateLimitRPS:     vk.RateLimitRPS,
		RateLimitBurst:   vk.RateLimitBurst,
		RateLimitTPM:     vk.RateLimitTPM,
		AllowedProviders: vk.AllowedProviders,
		StripReasoning:   vk.StripReasoning,
	}
	if vk.Owner != nil && vk.OwnerUserID != nil {
		info.Owner = &OwnerInfo{
			ID:               vk.OwnerUserID.String(),
			Enabled:          vk.Owner.Enabled,
			RateLimitRPS:     vk.Owner.RateLimitRPS,
			RateLimitBurst:   vk.Owner.RateLimitBurst,
			RateLimitTPM:     vk.Owner.RateLimitTPM,
			AllowedProviders: vk.Owner.AllowedProviders,
		}
	}
	return info, nil
}

func (a *virtualKeyRepoAdapter) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error) {
	// The proxy never creates owned keys; ownership is a dashboard concern.
	vk, err := a.repo.Create(ctx, name, keyHash, keyPreview, rps, burst, tpm, allowedProviders, stripReasoning, nil)
	if err != nil {
		return nil, err
	}
	return &VirtualKeyInfo{
		ID:               vk.ID.String(),
		Name:             vk.Name,
		KeyHash:          vk.KeyHash,
		KeyPreview:       vk.KeyPreview,
		TokensUsed:       vk.TokensUsed,
		RateLimitRPS:     vk.RateLimitRPS,
		RateLimitBurst:   vk.RateLimitBurst,
		RateLimitTPM:     vk.RateLimitTPM,
		AllowedProviders: vk.AllowedProviders,
		StripReasoning:   vk.StripReasoning,
	}, nil
}

func (a *virtualKeyRepoAdapter) Delete(ctx context.Context, id string) error {
	vid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	return a.repo.Delete(ctx, vid)
}

// NewHandler creates a new proxy Handler.
func NewHandler(
	cfg *config.Config,
	providerRepo *provider.Repository,
	modelRepo ModelRepository,
	dbPool *pgxpool.Pool,
	virtualKeyRepo *virtualkey.Repository,
	failoverRepo *failover.Repository,
	settingsRepo *settings.Repository,
	rateLimiter *ratelimit.Limiter,
	tpmLimiter *ratelimit.TPMLimiter,
	ipLimiter *ratelimit.IPLimiter,
	sd *SafeDialer,
) *Handler {
	inflight := newInflightLimiter()
	metrics.RegisterInflightCollector(inflight.snapshot)
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		dbPool:         dbPool,
		virtualKeyRepo: &virtualKeyRepoAdapter{repo: virtualKeyRepo},
		failoverRepo:   failoverRepo,
		settingsRepo:   settingsRepo,
		rateLimiter:    rateLimiter,
		tpmLimiter:     tpmLimiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		inflight:       inflight,
		shutdown:       make(chan struct{}),
		upstreamTransport: &http.Transport{
			DialContext:           safeDialFunc(sd),
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: sd,
	}
}

// safeDialFunc returns sd.DialContext if sd is non-nil, otherwise nil
// (which makes http.Transport use the default dialer).
func safeDialFunc(sd *SafeDialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if sd != nil {
		return sd.DialContext
	}
	return nil
}

// Close releases resources owned by the handler. Call during server
// shutdown so the shared upstream Transport terminates its idle
// connection goroutines.
func (h *Handler) Close() {
	if h.shutdown != nil {
		h.shutdownOnce.Do(func() { close(h.shutdown) })
	}
	if h.upstreamTransport != nil {
		h.upstreamTransport.CloseIdleConnections()
		debuglog.Info("proxy: closed upstream transport")
	}
}

// Register mounts the OpenAI-compatible surface. afterAuth are the caller's
// middlewares that must see only authenticated requests: the server's
// body-peeking timeout middleware buffers the whole request body (up to
// MAX_REQUEST_SIZE) to read the stream flag and the model, and mounting it
// ahead of the key check let an unauthenticated client make the gateway hold
// that allocation for the duration of its upload. They run after the key is
// verified and before the per-key limiters, which read nothing from the body.
// The gateway itself then reads nothing of an unauthenticated body; net/http
// still discards up to 256 KiB of it after the 401, under the body read
// deadline, so that is the bound on how long such a connection is held.
func (h *Handler) Register(r chi.Router, afterAuth ...func(http.Handler) http.Handler) {
	r.Use(h.ipLimiter.Middleware)
	r.Use(h.ProxyKeyMiddleware)
	r.Use(afterAuth...)
	r.Use(h.rateLimiter.Middleware(h.cfg.RateLimitEnabled))
	// TPM admission runs after RPS: a key must pass the request-rate gate before
	// its token budget is checked. This is the full two-stage gate (per-key
	// budget, plus the owner's aggregate budget when the key is owned); admin
	// chat has no virtual key and so runs only the owner stage, see
	// RegisterAdminChat.
	r.Use(h.tpmLimiter.Middleware(h.cfg.RateLimitEnabled))

	r.Get("/models", h.ListModels)
	r.Post("/chat/completions", h.ChatCompletions)
	r.Post("/messages", h.Messages) // native Anthropic Messages API surface
	r.Post("/embeddings", h.Embeddings)
	r.Post("/rerank", h.Rerank)
	r.Post("/images/generations", h.ImageGenerations)
	r.Post("/images/edits", h.ImageEdits)
	r.Post("/images/variations", h.ImageVariations)
	r.Post("/audio/speech", h.AudioSpeech)
	r.Post("/audio/transcriptions", h.AudioTranscriptions)
	r.Post("/audio/translations", h.AudioTranslations)
}

// RegisterAdminChat adds the admin chat endpoint.
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
	// TPM admission after RPS, mirroring Register. Only the owner stage applies
	// here: this surface authenticates a dashboard session, so there is no
	// virtual key to carry a per-key budget, and the per-key stage's fallback
	// bucket (the resolved client address) has no debit path. The caller's own aggregate cap is
	// published by api.ChatUserContextMiddleware, which main.go mounts ahead of
	// this group; without both middlewares a user with a TPM cap would meter
	// nothing here while their /v1 traffic is capped.
	r.Use(h.tpmLimiter.UserMiddleware(h.cfg.RateLimitEnabled))

	r.Post("/chat", h.ChatCompletions)
	r.Post("/arena", h.ChatCompletions)
	r.Post("/completions", h.ChatCompletions)
}

// keyLookupTimeout bounds the virtual-key lookup: a primary-key read that is
// not back in ten seconds is a database that is not answering, and the
// request is refused as an internal error rather than parked.
const keyLookupTimeout = 10 * time.Second

// ProxyKeyMiddleware validates the virtual API key in the request header.
func (h *Handler) ProxyKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A refusal closes the connection. net/http drains an unread request
		// body before it sends a keep-alive response, so without this a
		// client trickling a body with no valid key was told 401 only when
		// the body deadline cut the drain; a caller with no key has no
		// keep-alive worth keeping. The IP limiter's 429 ahead of this check
		// deliberately does not close: it also fronts the dashboard routes,
		// where a throttled client is legitimate and retries on the same
		// connection.
		refuse := func(msg string) {
			w.Header().Set("Connection", "close")
			writeOpenAIError(w, msg, http.StatusUnauthorized)
		}
		token, ok := util.ParseProxyKey(r)
		if !ok {
			// Client error, not a server fault — Warn keeps the Error stream
			// reserved for things the operator must act on.
			debuglog.Warn("auth: missing authorization header", "remote_addr", clientip.From(r))
			refuse("missing authorization header: expected \"Authorization: Bearer <virtual key>\" or \"x-api-key: <virtual key>\"")
			return
		}

		keyHash := virtualkey.Hash(token)
		// Bounded on its own: the timeout middleware used to wrap this lookup
		// and now runs behind it, so a wedged database must not park every
		// request here until the client gives up.
		lookupCtx, lookupCancel := context.WithTimeout(r.Context(), keyLookupTimeout)
		vk, err := h.virtualKeyRepo.FindByKeyHash(lookupCtx, keyHash)
		lookupCancel()
		if err != nil {
			if errors.Is(err, virtualkey.ErrNotFound) {
				debuglog.Warn("auth: key not found", "remote_addr", clientip.From(r))
				refuse("invalid virtual key")
			} else if errors.Is(err, context.Canceled) {
				// The client left mid-lookup: nothing to answer, and not the
				// database's fault, so not the Error stream.
				debuglog.Warn("auth: key lookup abandoned, client gone", "remote_addr", clientip.From(r))
				return
			} else {
				debuglog.Error("auth: db lookup failed", "error", err)
				writeOpenAIError(w, "internal error", http.StatusInternalServerError)
			}
			return
		}
		if vk.Owner != nil && !vk.Owner.Enabled {
			// Owned keys inherit the account's enabled switch: disabling a
			// user must cut their proxy traffic, not just their dashboard
			// login. The key itself stays intact for when the account
			// returns.
			debuglog.Warn("auth: key owner disabled", "remote_addr", clientip.From(r), "key", vk.Name)
			refuse("virtual key disabled: owner account is disabled")
			return
		}
		debuglog.Info("auth: authenticated", "key", vk.Name)
		ctx := context.WithValue(r.Context(), virtualKeyNameKey, vk.Name)
		ctx = context.WithValue(ctx, virtualKeyIDKey, vk.ID)
		ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitRPSKey, vk.RateLimitRPS)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitBurstKey, vk.RateLimitBurst)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitTPMKey, vk.RateLimitTPM)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyAllowedProvidersKey, vk.AllowedProviders)
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyStripReasoningKey, vk.StripReasoning)
		if vk.Owner != nil {
			ctx = context.WithValue(ctx, ctxkeys.VirtualKeyOwnerIDKey, vk.Owner.ID)
			ctx = context.WithValue(ctx, ctxkeys.UserRateLimitRPSKey, vk.Owner.RateLimitRPS)
			ctx = context.WithValue(ctx, ctxkeys.UserRateLimitBurstKey, vk.Owner.RateLimitBurst)
			ctx = context.WithValue(ctx, ctxkeys.UserRateLimitTPMKey, vk.Owner.RateLimitTPM)
			ctx = context.WithValue(ctx, ctxkeys.UserAllowedProvidersKey, vk.Owner.AllowedProviders)
		}
		debuglog.Debug("proxy: virtual key auth", "key", vk.Name, "strip_reasoning", vk.StripReasoning)
		// Fire-and-forget touch with a timeout so the goroutine cannot
		// outlive the server if the DB is slow.
		//nolint:gosec // intentional: periodic cache refresh is not request-scoped
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

// CircuitBreaker returns the circuit breaker instance for read-only status access.
func (h *Handler) CircuitBreaker() *failover.CircuitBreaker {
	return h.circuitBreaker
}

// CapLedger is the per-provider last-exhausted-429 ledger: written by the
// 429 judgement, overlaid onto the provider list by the admin API.
func (h *Handler) CapLedger() *provider.CapLedger {
	h.capLedgerOnce.Do(func() { h.capLedger = provider.NewCapLedger() })
	return h.capLedger
}
