package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// TotpHandler exposes the TOTP (RFC 6238) second-factor endpoints: public
// status + login, and admin/session-gated enroll/verify/disable. The login
// endpoint exchanges a valid (admin token + TOTP code) pair for a session
// token that authenticates subsequent API calls once 2FA is enabled.
type TotpHandler struct {
	totpRepo           *totp.Repository
	adminMgr           AdminAuthenticator
	sessionMgr         *webauthn.SessionManager
	ipLimiter          IPLimiterMiddleware
	demoReadOnly       bool
	totpEnabled        func() bool           // shared cached state (Handler.TotpEnabled)
	refreshTotpEnabled func(context.Context) // refresh cache after mutations (Handler.RefreshTotpEnabled)
	loginThrottle      *totp.Throttle        // per-IP exponential backoff on failed /totp/login
	// confirmed_at cache for /totp/status. The stamp is set once at enrollment
	// and never changes until disable/re-enroll, so a polled status endpoint can
	// serve it from memory instead of reading the DB on every call. Read lock-free
	// off the hot path; populated lazily on the first enabled read. enabledAtGen
	// bumps on every enable/disable so a read whose DB fetch raced an invalidation
	// declines to publish its now-stale value (see publishEnabledAt). The DB fetch
	// itself runs without enabledAtMu, so a slow status read never blocks a
	// concurrent enroll/disable.
	enabledAtCache atomic.Pointer[time.Time]
	enabledAtGen   atomic.Uint64
	enabledAtMu    sync.Mutex
}

// NewTotpHandler constructs a TotpHandler wired to the shared TOTP-enabled cache.
func NewTotpHandler(
	totpRepo *totp.Repository,
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	ipLimiter IPLimiterMiddleware,
	demoReadOnly bool,
	totpEnabled func() bool,
	refreshTotpEnabled func(context.Context),
) *TotpHandler {
	return &TotpHandler{
		totpRepo:           totpRepo,
		adminMgr:           adminMgr,
		sessionMgr:         sessionMgr,
		ipLimiter:          ipLimiter,
		demoReadOnly:       demoReadOnly,
		totpEnabled:        totpEnabled,
		refreshTotpEnabled: refreshTotpEnabled,
		// After maxFailures failed logins from one IP, back off exponentially
		// (1s doubling, capped at 5m), self-clearing and reset on success.
		loginThrottle: totp.NewThrottle(5, time.Second, 5*time.Minute),
	}
}

// Register mounts the TOTP routes on the given router.
func (h *TotpHandler) Register(r chi.Router) {
	r.Route("/totp", func(r chi.Router) {
		r.Get("/status", h.Status)
		r.Post("/login", h.Login)
		r.Group(func(r chi.Router) {
			if h.demoReadOnly {
				r.Use(readOnlyGuard)
			}
			r.Use(h.adminOrSessionAuth)
			r.Get("/info", h.Info)
			r.Post("/enroll/start", h.EnrollStart)
			r.Post("/enroll/verify", h.EnrollVerify)
			r.Post("/disable", h.Disable)
		})
	})
}

// adminOrSessionAuth validates either the admin token or a session token for
// TOTP mutation routes. Mirrors Handler.AuthMiddleware's gate: when TOTP is
// enabled, the raw admin token is a first factor only and must not unlock
// enroll/disable, so the second factor cannot be bypassed.
func (h *TotpHandler) adminOrSessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := util.ParseBearerToken(r)
		if !ok {
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}
		// Raw admin token only when TOTP disabled (mirrors Handler.AuthMiddleware).
		if (h.totpEnabled == nil || !h.totpEnabled()) && h.adminMgr.Validate(token) {
			next.ServeHTTP(w, r)
			return
		}
		if h.sessionMgr != nil && h.sessionMgr.Validate(r.Context(), token) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Invalid admin token or session token", http.StatusUnauthorized)
	})
}

// statusResponse is the GET /api/totp/status payload. EnabledAt is the RFC3339
// confirmation time, omitted when TOTP is disabled.
type statusResponse struct {
	Enabled   bool   `json:"enabled"`
	EnabledAt string `json:"enabled_at,omitempty"`
}

// Status reports the TOTP-enabled state. The enabled flag comes from the shared
// cached value so the login UI's view matches what AuthMiddleware enforces; when
// enabled it also surfaces confirmed_at (served from the in-memory cache, so a
// polled status endpoint stays DB-free on the hot path) for the settings panel.
func (h *TotpHandler) Status(w http.ResponseWriter, r *http.Request) {
	enabled := h.totpEnabled != nil && h.totpEnabled()
	resp := statusResponse{Enabled: enabled}
	if enabled {
		resp.EnabledAt = h.cachedEnabledAt(r.Context())
	}
	writeJSON(w, resp)
}

// infoResponse is the GET /api/totp/info payload for the settings panel:
// recovery-code usage and last-used time. Kept separate from the public,
// polled /totp/status so those per-request DB reads stay off the hot path.
type infoResponse struct {
	RecoveryRemaining int    `json:"recovery_remaining"`
	RecoveryTotal     int    `json:"recovery_total"`
	LastUsedAt        string `json:"last_used_at,omitempty"`
}

// Info reports recovery-code usage and the last-used time for the current
// enrollment. Admin/session gated (unlike Status) since it exposes recovery
// state; the settings panel reads it once rather than polling it.
func (h *TotpHandler) Info(w http.ResponseWriter, r *http.Request) {
	if h.totpRepo == nil {
		writeJSON(w, infoResponse{})
		return
	}
	si, err := h.totpRepo.Info(r.Context())
	if err != nil {
		respondError(w, "failed to read TOTP info", err, http.StatusInternalServerError)
		return
	}
	resp := infoResponse{
		RecoveryRemaining: si.RecoveryRemaining,
		RecoveryTotal:     si.RecoveryTotal,
	}
	if !si.LastUsed.IsZero() {
		resp.LastUsedAt = si.LastUsed.UTC().Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

// cachedEnabledAt returns the RFC3339 confirmation time, reading the DB at most
// once per enable: the value never changes while TOTP stays enabled, so it is
// memoized and cleared on enable/disable. Returns "" when unknown (no repo, or a
// transient read error), in which case the field is omitted and the next call
// retries rather than caching the miss.
func (h *TotpHandler) cachedEnabledAt(ctx context.Context) string {
	if cached := h.enabledAtCache.Load(); cached != nil {
		return cached.UTC().Format(time.RFC3339)
	}
	if h.totpRepo == nil {
		return ""
	}
	// Snapshot the generation before the read. The DB fetch runs without the lock
	// (so it can't block a concurrent enroll/disable); publishEnabledAt then stores
	// the result only if no enable/disable bumped the generation meanwhile.
	gen := h.enabledAtGen.Load()
	at, ok, err := h.totpRepo.EnabledAt(ctx)
	if err != nil || !ok {
		return ""
	}
	h.publishEnabledAt(gen, at)
	return at.UTC().Format(time.RFC3339)
}

// publishEnabledAt caches at only if no enable/disable happened since gen was
// sampled, so an in-flight read of a since-replaced enrollment cannot overwrite a
// newer invalidation. The lock makes the generation check and the store atomic
// against invalidateEnabledAt.
func (h *TotpHandler) publishEnabledAt(gen uint64, at time.Time) {
	h.enabledAtMu.Lock()
	if h.enabledAtGen.Load() == gen {
		h.enabledAtCache.Store(&at)
	}
	h.enabledAtMu.Unlock()
}

// invalidateEnabledAt clears the cached confirmed_at and advances the generation
// so any read already in flight declines to publish its now-stale value.
func (h *TotpHandler) invalidateEnabledAt() {
	h.enabledAtMu.Lock()
	h.enabledAtGen.Add(1)
	h.enabledAtCache.Store(nil)
	h.enabledAtMu.Unlock()
}

// EnrollStart generates a new TOTP secret and returns the otpauth URI + secret.
func (h *TotpHandler) EnrollStart(w http.ResponseWriter, r *http.Request) {
	// Refuse re-enrollment while TOTP is active: rotating the secret requires
	// disabling first (the UI only offers Enable when disabled). Allowing it
	// here would flip the enforcement gate off while the new secret is staged --
	// a window where the raw admin token bypasses the second factor on every
	// protected endpoint -- and could also strand the admin if abandoned. Enroll
	// therefore only ever runs from the disabled state, so the gate never moves.
	if h.totpEnabled != nil && h.totpEnabled() {
		respondError(w, "disable TOTP before re-enrolling", nil, http.StatusConflict)
		return
	}
	uri, secret, err := h.totpRepo.Enroll(r.Context())
	if err != nil {
		respondError(w, "totp: enroll failed", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"uri": uri, "secret": secret})
}

// EnrollVerify verifies the TOTP code, enables 2FA, and returns recovery codes.
func (h *TotpHandler) EnrollVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	ok, err := h.totpRepo.Verify(r.Context(), req.Code)
	if err != nil {
		respondError(w, "totp: verify failed", err, http.StatusInternalServerError)
		return
	}
	if !ok {
		respondBadRequest(w, "invalid TOTP code", nil)
		return
	}
	// Generate recovery codes BEFORE enabling: if this fails, 2FA stays off and
	// the user retries cleanly instead of being left enabled with no codes.
	codes, err := h.totpRepo.GenerateRecoveryCodes(r.Context())
	if err != nil {
		respondError(w, "totp: recovery codes failed", err, http.StatusInternalServerError)
		return
	}
	if err := h.totpRepo.Enable(r.Context()); err != nil {
		respondError(w, "totp: enable failed", err, http.StatusInternalServerError)
		return
	}
	// Refresh cache AFTER Enable so the hot path starts rejecting raw admin
	// tokens immediately.
	h.refreshTotpEnabled(r.Context())
	// Drop the stale confirmed_at so the next status read picks up this
	// enrollment's fresh stamp.
	h.invalidateEnabledAt()
	// Mint a session token so the admin who just enabled 2FA stays logged in.
	// Enabling invalidates the raw admin token their browser was using, so
	// without this the dashboard's next calls 401 and it looks like the app
	// crashed. Both factors were proven here (admin token reached this gated
	// endpoint + a valid TOTP code), so a session is warranted. Best effort:
	// the recovery codes are the critical payload and are returned even if the
	// mint fails (the admin can re-login manually).
	resp := map[string]any{"recovery_codes": codes}
	if h.sessionMgr != nil {
		if tok, err := h.sessionMgr.CreateAuthToken(r.Context(), []byte("admin"), nil); err != nil {
			debuglog.Error("totp: failed to mint post-enroll session token; admin must re-login", "error", err)
		} else {
			resp["token"] = tok
		}
	}
	writeJSON(w, resp)
}

// Disable removes the TOTP config + recovery codes. Requires a valid current
// TOTP or recovery code as a safeguard (401 on mismatch, not 400).
func (h *TotpHandler) Disable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	// Authorize and disable atomically: the code is only spent if the whole
	// disable commits, so a transient DB error cannot consume a recovery code
	// (or burn a TOTP step) while leaving TOTP enabled.
	ok, err := h.totpRepo.DisableWithCode(r.Context(), req.Code)
	if err != nil {
		respondError(w, "totp: disable failed", err, http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "invalid TOTP or recovery code", http.StatusUnauthorized)
		return
	}
	h.refreshTotpEnabled(r.Context())
	// Clear the cached stamp so a later re-enrollment doesn't serve this one.
	h.invalidateEnabledAt()
	writeJSON(w, map[string]bool{"disabled": true})
}

// Login exchanges a valid (admin token + TOTP code) pair for a session token.
func (h *TotpHandler) Login(w http.ResponseWriter, r *http.Request) {
	// The endpoint is only meaningful when TOTP is active.
	if h.totpEnabled == nil || !h.totpEnabled() {
		respondError(w, "TOTP is not enabled", nil, http.StatusBadRequest)
		return
	}
	// Per-IP failure backoff (defense in depth atop the /api per-IP rate limit):
	// refuse before doing any work while this key is locked.
	throttleKey := h.ipLimiter.ClientIP(r)
	if ok, retry := h.loginThrottle.Allowed(throttleKey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		debuglog.Warn("totp: login throttled", "remote_addr", r.RemoteAddr)
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}
	var req struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	// Check the admin-token first factor, then the second factor ONLY when the
	// token is valid: a wrong token must not consume a single-use TOTP step
	// (RFC 6238 §5.2) or burn a recovery code. Verify atomically consumes the
	// matched TOTP step; a recovery code is the fallback. This is deliberately
	// not constant-time w.r.t. which factor failed -- protecting the single-use
	// factors from a no-auth attacker matters more than token-vs-code timing.
	tokenValid := h.adminMgr.Validate(req.Token)
	codeValid := false
	if tokenValid {
		if ok, err := h.totpRepo.Verify(r.Context(), req.Code); err == nil && ok {
			codeValid = true
		} else {
			if err != nil {
				debuglog.Error("totp: login verify failed", "error", err, "remote_addr", r.RemoteAddr)
			}
			if ok, _ := h.totpRepo.ConsumeRecoveryCode(r.Context(), req.Code); ok {
				codeValid = true
			}
		}
	}
	if !tokenValid || !codeValid {
		h.loginThrottle.RecordFailure(throttleKey)
		debuglog.Warn("totp: login failed", "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid admin token or TOTP code", http.StatusUnauthorized)
		return
	}
	// Mint a session token reusing the passkey session infrastructure. credential_id
	// is nil (nullable BYTEA, no FK): not tied to a passkey, so not cascade-revoked.
	if h.sessionMgr == nil {
		debuglog.Error("totp: login called with no session manager wired")
		http.Error(w, "session infrastructure not available", http.StatusInternalServerError)
		return
	}
	sessionToken, err := h.sessionMgr.CreateAuthToken(r.Context(), []byte("admin"), nil)
	if err != nil {
		debuglog.Error("totp: login session creation failed", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	h.loginThrottle.RecordSuccess(throttleKey)
	writeJSON(w, map[string]string{"token": sessionToken})
}
