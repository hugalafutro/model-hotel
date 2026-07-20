package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// UserLoginStore is the slice of the user repository the login endpoint needs.
// Implemented by *user.Repository.
type UserLoginStore interface {
	GetByUsername(ctx context.Context, username string) (*user.User, error)
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
	HasEnabled(ctx context.Context) (bool, error)
}

// UserTotpFactory builds a per-user TOTP repository (Store bound to that
// user's rows). Implemented in main.go as a closure over the pool and
// MASTER_KEY; nil when per-user TOTP is not wired (tests). Aliased to
// totp.UserFactory so both login and the self-service API share one signature.
type UserTotpFactory = totp.UserFactory

// UserLoginHandler exposes the multi-user password login: a public status
// endpoint (does the login UI show the username/password form at all?) and
// the login exchange itself, which mints the same session tokens as every
// other login front-end (passkey/TOTP/OIDC/GitHub) so no downstream gate
// changes. The env admin token stays outside this flow as break-glass.
type UserLoginHandler struct {
	users      UserLoginStore
	sessionMgr *webauthn.SessionManager
	ipLimiter  IPLimiterMiddleware
	userTotp   UserTotpFactory
	// cookieSecure ("auto"/"always"/"never") resolves the Secure attribute on
	// the session cookie so plain-http LAN deployments still work.
	cookieSecure string
	// Per-IP exponential backoff on failed logins, same knobs as /totp/login.
	throttle *totp.Throttle
	// Per-username backoff so a brute force distributed across source IPs is
	// still slowed per target account. Backoff only (never a hard lock), so an
	// attacker hammering a username delays its owner at most maxDelay.
	userThrottle *totp.Throttle
}

// NewUserLoginHandler constructs the password-login front-end. userTotp may
// be nil (no second factor is ever required then).
func NewUserLoginHandler(users UserLoginStore, sessionMgr *webauthn.SessionManager, ipLimiter IPLimiterMiddleware, userTotp UserTotpFactory, cookieSecure string) *UserLoginHandler {
	return &UserLoginHandler{
		users:        users,
		sessionMgr:   sessionMgr,
		ipLimiter:    ipLimiter,
		userTotp:     userTotp,
		cookieSecure: cookieSecure,
		throttle:     totp.NewThrottle(5, time.Second, 5*time.Minute),
		userThrottle: totp.NewThrottle(5, time.Second, 5*time.Minute),
	}
}

// Register mounts the public auth routes.
func (h *UserLoginHandler) Register(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Get("/status", h.Status)
		r.Post("/login", h.Login)
	})
}

// Status tells the login UI whether any enabled user accounts exist. Public
// and boolean-only by design: it leaks nothing beyond "the form is useful".
func (h *UserLoginHandler) Status(w http.ResponseWriter, r *http.Request) {
	enabled, err := h.users.HasEnabled(r.Context())
	if err != nil {
		// Fail quiet: the login UI simply hides the form; the env admin token
		// and other login front-ends still work.
		debuglog.Error("userlogin: status query failed", "error", err)
		enabled = false
	}
	writeJSON(w, map[string]bool{"enabled": enabled})
}

// dummyHash is verified against when the username does not exist, so a
// missing user costs the same argon2 work as a wrong password and the
// endpoint does not leak which usernames exist through response timing.
var dummyHash = sync.OnceValue(func() string {
	hash, err := user.HashPassword("model-hotel-timing-equalizer")
	if err != nil {
		// rand.Read failing means the process is in a bad state; the login
		// path below will fail closed on CreateAuthToken anyway.
		debuglog.Error("userlogin: failed to precompute dummy hash", "error", err)
		return ""
	}
	return hash
})

// Login exchanges username+password for a session token. Uniform 401 for
// unknown user, wrong password, and disabled account; per-IP and per-username
// backoff on failures.
func (h *UserLoginHandler) Login(w http.ResponseWriter, r *http.Request) {
	throttleKey := h.ipLimiter.ClientIP(r)
	if ok, retry := h.throttle.Allowed(throttleKey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		debuglog.Warn("userlogin: throttled", "remote_addr", r.RemoteAddr)
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		// TOTP or recovery code; required only when the account has 2FA
		// enabled (the missing-code response tells the login UI to ask).
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	if req.Username == "" || req.Password == "" {
		respondBadRequest(w, "username and password are required", nil)
		return
	}

	// Per-target-account backoff: without this, a brute force spread across
	// source IPs never trips the per-IP throttle above.
	userKey := "user:" + req.Username
	if ok, retry := h.userThrottle.Allowed(userKey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		debuglog.Warn("userlogin: account throttled", "remote_addr", r.RemoteAddr)
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}

	u, err := h.users.GetByUsername(r.Context(), req.Username)
	if err != nil && !errors.Is(err, user.ErrNotFound) {
		respondError(w, "login failed", err, http.StatusInternalServerError)
		return
	}

	hash := dummyHash()
	if u != nil {
		hash = u.PasswordHash
	}
	ok, verr := user.VerifyPassword(req.Password, hash)
	if verr != nil && u != nil {
		// A malformed stored hash is corruption, not bad credentials.
		debuglog.Error("userlogin: stored hash malformed", "username", req.Username, "error", verr)
	}
	if u == nil || !ok || !u.Enabled {
		h.throttle.RecordFailure(throttleKey)
		h.userThrottle.RecordFailure(userKey)
		debuglog.Warn("userlogin: login failed", "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	if proceed := h.checkSecondFactor(w, r, u, req.Code, throttleKey, userKey); !proceed {
		return
	}

	token, err := h.sessionMgr.CreateAuthToken(r.Context(), []byte(u.ID.String()), nil)
	if err != nil {
		debuglog.Error("userlogin: session creation failed", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	h.throttle.RecordSuccess(throttleKey)
	h.userThrottle.RecordSuccess(userKey)
	if err := h.users.TouchLastLogin(r.Context(), u.ID); err != nil {
		debuglog.Warn("userlogin: failed to record last login", "error", err)
	}
	// Hand the session to the browser as an HttpOnly cookie rather than in the
	// body: JS never touches it, and the cookie MaxAge is bound to the same
	// webauthn.AuthTokenTTL as the server-side session so the two cannot drift.
	if err := authcookie.SetSession(w, token, authcookie.Secure(r, h.cookieSecure), webauthn.AuthTokenTTL); err != nil {
		debuglog.Error("userlogin: set session cookie failed", "error", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// checkSecondFactor enforces per-user TOTP after the password has verified.
// Returns true when login may proceed (no TOTP wired, not enabled, or a valid
// TOTP/recovery code). A missing code answers 401 {"totp_required": true} —
// revealed only behind a correct password, so it leaks nothing to guessers —
// without recording a throttle failure (the password was right; the UI is
// just being told to ask for the code). A wrong code counts against both
// throttles like any other failed attempt. Verification consumes the matched
// step (single-use); a recovery code is the atomic fallback.
func (h *UserLoginHandler) checkSecondFactor(w http.ResponseWriter, r *http.Request, u *user.User, code, throttleKey, userKey string) bool {
	if h.userTotp == nil {
		return true
	}
	repo := h.userTotp(u.ID)
	enabled, err := repo.IsEnabled(r.Context())
	if err != nil {
		// Fail closed: an unreadable TOTP state must not skip the second factor.
		respondError(w, "login failed", err, http.StatusInternalServerError)
		return false
	}
	if !enabled {
		return true
	}
	if code == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]bool{"totp_required": true})
		return false
	}
	if ok, verr := repo.Verify(r.Context(), code); verr == nil && ok {
		return true
	} else if verr != nil {
		debuglog.Error("userlogin: totp verify failed", "error", verr)
	}
	if ok, cerr := repo.ConsumeRecoveryCode(r.Context(), code); cerr == nil && ok {
		debuglog.Info("userlogin: recovery code used", "username", u.Username)
		return true
	} else if cerr != nil {
		debuglog.Error("userlogin: recovery code check failed", "error", cerr)
	}
	h.throttle.RecordFailure(throttleKey)
	h.userThrottle.RecordFailure(userKey)
	debuglog.Warn("userlogin: invalid TOTP code", "remote_addr", r.RemoteAddr)
	http.Error(w, "invalid TOTP code", http.StatusUnauthorized)
	return false
}
