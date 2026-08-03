package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/proxy"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// chatUserCtxEnv is the admin chat surface assembled the way main.go assembles
// it: the real AuthMiddleware, the real chat grant check, the real user-context
// middleware and the real proxy chat handler (rate limiters included), in front
// of a provider that actually answers. The point is that nothing between the
// session and the upstream is faked, so a 200 here means the request genuinely
// reached a provider, and a 429 means a real limiter refused it.
type chatUserCtxEnv struct {
	Router     chi.Router
	Handler    *Handler
	UserRepo   *user.Repository
	LoginAs    func(userID string) string
	ProviderID uuid.UUID
	// Model is the "<provider>/<model>" string the chat body asks for.
	Model string
}

func setupChatUserCtxTest(t *testing.T) *chatUserCtxEnv {
	t.Helper()
	ctx := context.Background()

	h := newTestHandler(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(ctx, `TRUNCATE users, webauthn_sessions, virtual_keys, models, providers CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sessionMgr)
	h.SetUserAuth(userRepo, webauthnRepo)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer chat-cap-upstream-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-cap",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   body["model"],
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	t.Cleanup(upstream.Close)

	suffix := uuid.New().String()[:8]
	providerName := "chat-cap-provider-" + suffix
	kp, err := auth.Encrypt("chat-cap-upstream-key", testMasterKey)
	if err != nil {
		t.Fatalf("encrypt provider key: %v", err)
	}
	var providerID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled)
		VALUES ($1, $2, $3, $4, $5, 'sk-***', true, false) RETURNING id`,
		providerName, upstream.URL, kp.Ciphertext, kp.Nonce, kp.Salt).Scan(&providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	modelRepo := model.NewRepository(pool)
	modelName := "chat-cap-model-" + suffix
	if err := modelRepo.Upsert(ctx, &model.Model{
		ID:               uuid.New(),
		ProviderID:       providerID,
		ModelID:          modelName,
		Name:             "Chat Cap Model",
		Capabilities:     "{}",
		Params:           "{}",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}); err != nil {
		t.Fatalf("seed model: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)
	// Pin the rate-limit settings this surface reads instead of inheriting
	// whatever another test in this package last wrote. The global stages are
	// opened wide so the only thing that can produce a 429 below is a per-user
	// cap, and max_wait is zeroed so an over-cap request is refused instead of
	// slept through (the limiter serves a delay <= max_wait after sleeping).
	for k, v := range map[string]string{
		"rate_limit_enabled":     "true",
		"rate_limit_rps":         "1000",
		"rate_limit_burst":       "1000",
		"rate_limit_max_wait_ms": "0",
	} {
		if err := settingsRepo.Set(ctx, k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	proxyHandler := proxy.NewHandler(
		// RateLimitEnabled is the env kill-switch both limiters check first.
		// It defaults to true in production (config.Load), so leaving it false
		// here would quietly turn RegisterAdminChat's rate-limit middlewares
		// into no-ops and make every limit assertion below vacuous.
		&config.Config{MasterKey: testMasterKey, RateLimitEnabled: true},
		provider.NewRepository(pool),
		modelRepo,
		pool,
		virtualkey.NewRepository(pool),
		failover.NewRepository(pool),
		settingsRepo,
		ratelimit.NewLimiter(settingsRepo),
		ratelimit.NewTPMLimiter(settingsRepo),
		ratelimit.NewIPLimiter(1000, 1000, nil, nil),
		// The upstream is on loopback, which the SafeDialer blocks unless the
		// host is allowlisted.
		proxy.NewSafeDialer([]string{"127.0.0.1"}, nil),
	)
	t.Cleanup(proxyHandler.Close)

	r := chi.NewRouter()
	r.Route("/api/chat", func(r chi.Router) {
		r.Use(h.AuthMiddleware)
		r.Use(h.RequireGrant(user.GrantChat))
		r.Use(h.ChatUserContextMiddleware)
		proxyHandler.RegisterAdminChat(r)
	})

	return &chatUserCtxEnv{
		Router:     r,
		Handler:    h,
		UserRepo:   userRepo,
		ProviderID: providerID,
		Model:      providerName + "/" + modelName,
		LoginAs: func(userID string) string {
			token, err := sessionMgr.CreateAuthToken(context.Background(), []byte(userID), nil)
			if err != nil {
				t.Fatalf("CreateAuthToken: %v", err)
			}
			return token
		},
	}
}

// chatUser creates an enabled account holding the chat grant with the given
// account cap, written straight through the repository. Direct is deliberate:
// the users API refuses a non-nil empty allowed_providers, but pruning the last
// provider out of a cap produces exactly that row (provider.PruneAllowLists), so
// the deny-all case cannot be reached through the API.
//
// The parameter is providerCap, not cap: cap is a Go builtin and shadowing it
// trips revive's redefines-builtin-id rule.
func (e *chatUserCtxEnv) chatUser(t *testing.T, name string, providerCap *[]string) *user.User {
	t.Helper()
	return e.chatUserWithLimits(t, name, user.Limits{}, providerCap)
}

// chatUserWithLimits is chatUser with the account's aggregate rate limits set.
// These are the same columns ProxyKeyMiddleware reads off a key's owner on /v1,
// so an account created here is limited identically on both surfaces.
func (e *chatUserCtxEnv) chatUserWithLimits(t *testing.T, name string, limits user.Limits, providerCap *[]string) *user.User {
	t.Helper()
	u, err := e.UserRepo.Create(context.Background(), name, name, nil, "x", user.RoleUser,
		[]string{string(user.GrantChat)}, limits, providerCap)
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return u
}

// adminChatRoutes is every path RegisterAdminChat mounts (internal/proxy/handler.go),
// and they are the only routes under /api/chat: nothing else registers into that
// group. All three POST to the same h.ChatCompletions today, so one middleware
// covers all three — but that is a property of the current wiring, not something
// the production code pins. Giving arena its own handler, or mounting it outside
// the group, would silently drop the cap on that surface. Asserting per route is
// what turns that into a test failure instead of a quiet regression.
var adminChatRoutes = []string{"/api/chat/chat", "/api/chat/arena", "/api/chat/completions"}

// chatAt drives a non-streaming completion at one dashboard chat route with the
// given bearer token.
func (e *chatUserCtxEnv) chatAt(t *testing.T, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":false}`, e.Model)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.Router.ServeHTTP(w, req)
	return w
}

// chat is chatAt on the plain Chat page route, for the cases that are about the
// cap rather than about route coverage.
func (e *chatUserCtxEnv) chat(t *testing.T, token string) *httptest.ResponseRecorder {
	t.Helper()
	return e.chatAt(t, token, "/api/chat/chat")
}

// An uncapped account is unaffected: nil allowed_providers means no cap, and the
// chat surface must not invent one. This is also the control for the denials
// below, so a 403 there cannot be produced by broken seeding or wiring.
func TestAdminChat_UncappedCallerReachesTheProvider(t *testing.T) {
	env := setupChatUserCtxTest(t)
	u := env.chatUser(t, "chat-cap-uncapped", nil)

	w := env.chat(t, env.LoginAs(u.ID.String()))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
}

// The gap this closes: `chat` is an ordinary assignable grant, so before the cap
// reached this surface a user capped to one provider could pick any other
// provider's model on the Chat page and reach it.
//
// Asserted on EVERY route RegisterAdminChat mounts, not just the one the Chat
// page uses. Arena and completions are separate paths that happen to share a
// handler; "happens to share a handler" is not something a test should be
// willing to assume on a security boundary.
func TestAdminChat_CappedCallerDeniedProviderOutsideTheCap(t *testing.T) {
	env := setupChatUserCtxTest(t)
	// The cap names some other provider entirely, so nothing serving this model
	// survives the filter.
	u := env.chatUser(t, "chat-cap-outside", &[]string{uuid.New().String()})
	token := env.LoginAs(u.ID.String())

	for _, path := range adminChatRoutes {
		t.Run(path, func(t *testing.T) {
			w := env.chatAt(t, token, path)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body %s", w.Code, w.Body.String())
			}
			// The refusal comes from the proxy's existing candidate filter, not
			// from a second bespoke rule that could drift away from the /v1
			// behaviour. It also rules out a 403 from some unrelated guard.
			if !strings.Contains(w.Body.String(), "does not have access to any provider") {
				t.Errorf("unexpected body: %s", w.Body.String())
			}
		})
	}
}

// The other half of the same account, on the same three routes: a provider
// inside the cap still works, so the cap narrows each surface rather than
// closing it. This is also what makes the denials above meaningful per route -
// it proves each path really does reach a provider when the cap allows it, so a
// 403 there is the filter refusing and not the route being broken or unmounted.
func TestAdminChat_CappedCallerReachesProviderInsideTheCap(t *testing.T) {
	env := setupChatUserCtxTest(t)
	u := env.chatUser(t, "chat-cap-inside", &[]string{env.ProviderID.String()})
	token := env.LoginAs(u.ID.String())

	for _, path := range adminChatRoutes {
		t.Run(path, func(t *testing.T) {
			w := env.chatAt(t, token, path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
			}
		})
	}
}

// A cap of {} denies every provider. Deleting the last provider named by a cap
// prunes it to an empty array rather than to NULL, and an empty non-nil list
// means "exactly these providers, of which there are none". If it were ever read
// as unrestricted, this account would silently gain full access.
func TestAdminChat_DenyAllCapReachesNothing(t *testing.T) {
	env := setupChatUserCtxTest(t)
	u := env.chatUser(t, "chat-cap-denyall", &[]string{})

	// Guard the premise: the row has to be an empty array, not NULL, or this
	// test would be asserting the wrong thing.
	stored, err := env.UserRepo.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.AllowedProviders == nil {
		t.Fatal("seeded deny-all cap came back NULL, i.e. unrestricted")
	}
	if len(*stored.AllowedProviders) != 0 {
		t.Fatalf("seeded cap = %v, want empty", *stored.AllowedProviders)
	}

	w := env.chat(t, env.LoginAs(u.ID.String()))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", w.Code, w.Body.String())
	}
}

// The env admin token has no users row at all, so there is no cap to apply and
// the chat surface stays fully open for it.
func TestAdminChat_EnvTokenAdminIsUnrestricted(t *testing.T) {
	env := setupChatUserCtxTest(t)

	w := env.chat(t, envAdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
}

// publishedUserCtx is every context value this middleware may publish, captured
// as `any` rather than as its concrete type on purpose: absent, present-but-nil
// and present-with-a-value are three different states to the consumers, and a
// typed field would collapse the first two.
type publishedUserCtx struct {
	providerCap any
	ownerID     any
	rps         any
	burst       any
	tpm         any
}

// userCtxProbe stands in for the proxy chat handler so the exact shapes
// published under the context keys can be asserted.
func userCtxProbe(reached *bool, got *publishedUserCtx) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		*reached = true
		ctx := r.Context()
		got.providerCap = ctx.Value(ctxkeys.UserAllowedProvidersKey)
		got.ownerID = ctx.Value(ctxkeys.VirtualKeyOwnerIDKey)
		got.rps = ctx.Value(ctxkeys.UserRateLimitRPSKey)
		got.burst = ctx.Value(ctxkeys.UserRateLimitBurstKey)
		got.tpm = ctx.Value(ctxkeys.UserRateLimitTPMKey)
	})
}

func probeRequest(t *testing.T, h *Handler, id *user.Identity) (*httptest.ResponseRecorder, bool, publishedUserCtx) {
	t.Helper()
	var reached bool
	var got publishedUserCtx
	req := httptest.NewRequest(http.MethodPost, "/api/chat/chat", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), id))
	w := httptest.NewRecorder()
	h.ChatUserContextMiddleware(userCtxProbe(&reached, &got)).ServeHTTP(w, req)
	return w, reached, got
}

// The env admin publishes NOTHING rather than an explicit "no limits": every key
// must stay absent so a surface that never ran this middleware and one that ran
// it for a limitless caller are indistinguishable to the consumers. It matters
// for the owner id in particular, which is not a cap at all: publishing an empty
// or placeholder one would give the limiters a bucket identity and the request
// log an owner that no account answers to.
func TestChatUserContextMiddleware_AdminIdentityPublishesNoKey(t *testing.T) {
	h := newTestHandler(t)
	h.SetUserAuth(user.NewRepository(h.Pool().Pool()), nil)

	w, reached, got := probeRequest(t, h, user.AdminIdentity())
	if !reached {
		t.Fatalf("admin request was blocked: %d %s", w.Code, w.Body.String())
	}
	for _, published := range []struct {
		key   string
		value any
	}{
		{"allowed providers", got.providerCap},
		{"owner id", got.ownerID},
		{"rate limit rps", got.rps},
		{"rate limit burst", got.burst},
		{"rate limit tpm", got.tpm},
	} {
		if published.value != nil {
			t.Errorf("admin identity published %s: %v", published.key, published.value)
		}
	}
}

// An uncapped user publishes a present-but-nil pointer, which the proxy reads as
// "this side restricts nothing". Asserting it here pins the difference from the
// deny-all shape, which is a nil *[]string away from being an escalation.
func TestChatUserContextMiddleware_PublishesTheCallersCap(t *testing.T) {
	h := newTestHandler(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	repo := user.NewRepository(pool)
	h.SetUserAuth(repo, nil)

	cases := []struct {
		name        string
		providerCap *[]string
		want        *[]string
	}{
		{"uncapped", nil, nil},
		{"capped", &[]string{"p1", "p2"}, &[]string{"p1", "p2"}},
		{"deny all", &[]string{}, &[]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := repo.Create(context.Background(), "probe-"+strings.ReplaceAll(tc.name, " ", "-"),
				"", nil, "x", user.RoleUser, []string{string(user.GrantChat)}, user.Limits{}, tc.providerCap)
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			id := &user.Identity{Role: user.RoleUser, Grants: []string{string(user.GrantChat)}, UserID: &u.ID, Username: u.Username}

			w, reached, published := probeRequest(t, h, id)
			if !reached {
				t.Fatalf("request was blocked: %d %s", w.Code, w.Body.String())
			}
			got, ok := published.providerCap.(*[]string)
			if !ok {
				t.Fatalf("context value = %#v, want a *[]string", published.providerCap)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("got %v, want nil (no cap)", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("got nil (no cap), want %v", *tc.want)
			case tc.want == nil:
				return
			}
			if len(*got) != len(*tc.want) {
				t.Fatalf("got %v, want %v", *got, *tc.want)
			}
			for i := range *tc.want {
				if (*got)[i] != (*tc.want)[i] {
					t.Fatalf("got %v, want %v", *got, *tc.want)
				}
			}
		})
	}
}

// Every way the cap can fail to resolve must stop the request. Serving it
// instead would serve it with no cap at all, which is the exact escalation this
// middleware exists to prevent, and unlike the virtual-key write path there is
// no foreign key underneath to catch it.
//
// These shapes are not reachable through AuthMiddleware, which rejects a session
// whose users row is missing and cannot mint a UserID identity without a store
// at all, so the identity is injected directly.
func TestChatUserContextMiddleware_FailsClosedWhenTheCapCannotBeRead(t *testing.T) {
	uid := uuid.New()
	id := &user.Identity{Role: user.RoleUser, Grants: []string{string(user.GrantChat)}, UserID: &uid, Username: "ghost"}

	cases := []struct {
		name  string
		store UserStore
		want  int
	}{
		{"no user store wired", nil, http.StatusInternalServerError},
		{"row deleted", failingUserStore{err: user.ErrNotFound}, http.StatusForbidden},
		{"store reports neither row nor error", failingUserStore{}, http.StatusForbidden},
		{"database unreadable", failingUserStore{err: errors.New("user store unavailable")}, http.StatusInternalServerError},
	}
	h := newTestHandler(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.SetUserAuth(tc.store, nil)

			w, reached, _ := probeRequest(t, h, id)
			if reached {
				t.Fatal("ESCALATION: the request reached the chat handler with no account limits resolved")
			}
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d; body %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// The account's aggregate rate limits and its own id have to reach the chat
// surface too, not just the provider cap: the limiters key their shared
// "user:<uuid>" buckets on the id and gate on the caps, and the request log
// stamps the id as the owner. Each is asserted in the shape its consumer reads,
// so a limit-carrying pointer and an absent key stay distinguishable.
func TestChatUserContextMiddleware_PublishesTheCallersLimits(t *testing.T) {
	h := newTestHandler(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	repo := user.NewRepository(pool)
	h.SetUserAuth(repo, nil)

	rps := 2.5
	burst := 7
	tpm := 900
	cases := []struct {
		name   string
		limits user.Limits
	}{
		{"unlimited", user.Limits{}},
		{"limited", user.Limits{RPS: &rps, Burst: &burst, TPM: &tpm}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := repo.Create(context.Background(), "limits-probe-"+tc.name, "", nil, "x",
				user.RoleUser, []string{string(user.GrantChat)}, tc.limits, nil)
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			id := &user.Identity{Role: user.RoleUser, Grants: []string{string(user.GrantChat)}, UserID: &u.ID, Username: u.Username}

			w, reached, got := probeRequest(t, h, id)
			if !reached {
				t.Fatalf("request was blocked: %d %s", w.Code, w.Body.String())
			}
			// The id is published for every account, limits or not: it is the
			// log/SSE attribution as well as the limiter bucket key.
			if got.ownerID != u.ID.String() {
				t.Errorf("owner id = %#v, want %q", got.ownerID, u.ID.String())
			}
			gotRPS, ok := got.rps.(*float64)
			if !ok {
				t.Fatalf("rps = %#v, want a *float64", got.rps)
			}
			gotBurst, ok := got.burst.(*int)
			if !ok {
				t.Fatalf("burst = %#v, want an *int", got.burst)
			}
			gotTPM, ok := got.tpm.(*int)
			if !ok {
				t.Fatalf("tpm = %#v, want an *int", got.tpm)
			}
			assertFloatPtr(t, "rps", gotRPS, tc.limits.RPS)
			assertIntPtr(t, "burst", gotBurst, tc.limits.Burst)
			assertIntPtr(t, "tpm", gotTPM, tc.limits.TPM)
		})
	}
}

// assertFloatPtr compares a published *float64 against the seeded one, keeping
// "no cap" (nil) distinct from a cap that happens to be zero.
func assertFloatPtr(t *testing.T, field string, got, want *float64) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %v, want nil (no cap)", field, *got)
	case want != nil && got == nil:
		t.Errorf("%s = nil (no cap), want %v", field, *want)
	case want != nil && *got != *want:
		t.Errorf("%s = %v, want %v", field, *got, *want)
	}
}

// assertIntPtr is assertFloatPtr for the *int limits.
func assertIntPtr(t *testing.T, field string, got, want *int) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %d, want nil (no cap)", field, *got)
	case want != nil && got == nil:
		t.Errorf("%s = nil (no cap), want %d", field, *want)
	case want != nil && *got != *want:
		t.Errorf("%s = %d, want %d", field, *got, *want)
	}
}

// A per-user tokens-per-minute cap has to bite on the dashboard Chat page, not
// just on /v1. It is the same account and the same budget, so a user whose /v1
// traffic is metered could otherwise spend unlimited provider tokens through the
// Chat page, which needs nothing but the ordinary `chat` grant.
//
// The budget is drained by real accounting, not by request count: the upstream
// stub reports 1 prompt + 1 completion token, and admission holds one further
// placeholder token per request (see TPMLimiter's doc comment), so a cap of 3
// admits exactly one request and refuses the next.
func TestAdminChat_PerUserTPMCapIsEnforced(t *testing.T) {
	env := setupChatUserCtxTest(t)
	tpm := 3
	u := env.chatUserWithLimits(t, "chat-tpm-capped", user.Limits{TPM: &tpm}, nil)
	token := env.LoginAs(u.ID.String())

	if w := env.chat(t, token); w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	w := env.chat(t, token)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429; body %s", w.Code, w.Body.String())
	}
	// The refusal must come from the owner-level TPM stage, not from the RPS
	// limiter or some unrelated guard that also answers 429.
	if !strings.Contains(w.Body.String(), "user token rate limit exceeded") {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

// An account with no TPM cap is untouched by the stage above: user-level caps
// have no global-settings fallback, so "no cap" must mean no budget at all
// rather than an implicit one. Without this the test above could pass simply
// because the chat surface throttles everyone.
func TestAdminChat_UncappedCallerIsNotTPMLimited(t *testing.T) {
	env := setupChatUserCtxTest(t)
	u := env.chatUser(t, "chat-tpm-uncapped", nil)
	token := env.LoginAs(u.ID.String())

	for i := range 3 {
		if w := env.chat(t, token); w.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200; body %s", i+1, w.Code, w.Body.String())
		}
	}
}

// The other half of gap 1: the per-user requests/sec cap. Unlike TPM this stage
// was already mounted on the chat routes (RegisterAdminChat has always used the
// RPS limiter); what was missing was the context it reads, so it silently
// applied to nobody.
//
// A burst of 1 at 0.5 rps means the second request would have to wait 2s, which
// exceeds the max_wait the environment pins to 0, so it is refused rather than
// slept through.
func TestAdminChat_PerUserRPSCapIsEnforced(t *testing.T) {
	env := setupChatUserCtxTest(t)
	rps := 0.5
	burst := 1
	u := env.chatUserWithLimits(t, "chat-rps-capped", user.Limits{RPS: &rps, Burst: &burst}, nil)
	token := env.LoginAs(u.ID.String())

	if w := env.chat(t, token); w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	w := env.chat(t, token)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429; body %s", w.Code, w.Body.String())
	}
	// "user rate limit exceeded" is the owner stage; the per-key stage says
	// "rate limit exceeded" and would mean the global bucket refused instead.
	if !strings.Contains(w.Body.String(), "user rate limit exceeded") {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

// Gap 2: a chat request has to be attributed to the account that made it, or the
// caller cannot see their own activity. The proxy stamps the owner onto every
// request lifecycle event from the same context key the limiters use, and
// eventVisible is what the SSE endpoint filters a non-admin's live feed with, so
// an unattributed chat request is invisible to the only person entitled to it.
//
// Asserted through eventVisible rather than by reading the metadata directly:
// the metadata is only interesting because that function consumes it.
func TestAdminChat_RequestIsAttributedToItsOwner(t *testing.T) {
	env := setupChatUserCtxTest(t)
	owner := env.chatUser(t, "chat-log-owner", nil)
	other := env.chatUser(t, "chat-log-bystander", nil)

	ownerID := logsIdentity(owner)
	otherID := logsIdentity(other)

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	if w := env.chat(t, env.LoginAs(owner.ID.String())); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}

	var mine []events.Event
drain:
	for {
		select {
		case ev := <-ch:
			// The bus is process-wide, so keep only the lifecycle events this
			// request produced. Matching on the owner is what makes that exact.
			if owner, _ := ev.Metadata["owner_user_id"].(string); owner == ownerID.UserID.String() {
				mine = append(mine, ev)
			}
		default:
			break drain
		}
	}
	if len(mine) == 0 {
		t.Fatal("the chat request published no lifecycle event carrying its owner: it is invisible to the caller's own log feed")
	}
	for _, ev := range mine {
		if !eventVisible(ownerID, ev) {
			t.Errorf("%s is invisible to the account that made the request", ev.Type)
		}
		if eventVisible(otherID, ev) {
			t.Errorf("LEAK: %s from one account's chat is visible to another", ev.Type)
		}
	}
}

// logsIdentity is the session identity of a logs-granted non-admin, the caller
// eventVisible scopes a live log feed for.
func logsIdentity(u *user.User) *user.Identity {
	return &user.Identity{
		Role:     user.RoleUser,
		Grants:   []string{string(user.GrantChat), string(user.GrantLogs)},
		UserID:   &u.ID,
		Username: u.Username,
	}
}
