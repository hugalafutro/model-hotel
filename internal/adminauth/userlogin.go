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

// UserLoginHandler exposes the multi-user password login: a public status
// endpoint (does the login UI show the username/password form at all?) and
// the login exchange itself, which mints the same session tokens as every
// other login front-end (passkey/TOTP/OIDC/GitHub) so no downstream gate
// changes. The env admin token stays outside this flow as break-glass.
type UserLoginHandler struct {
	users      UserLoginStore
	sessionMgr *webauthn.SessionManager
	ipLimiter  IPLimiterMiddleware
	// Per-IP exponential backoff on failed logins, same knobs as /totp/login.
	throttle *totp.Throttle
}

// NewUserLoginHandler constructs the password-login front-end.
func NewUserLoginHandler(users UserLoginStore, sessionMgr *webauthn.SessionManager, ipLimiter IPLimiterMiddleware) *UserLoginHandler {
	return &UserLoginHandler{
		users:      users,
		sessionMgr: sessionMgr,
		ipLimiter:  ipLimiter,
		throttle:   totp.NewThrottle(5, time.Second, 5*time.Minute),
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
// unknown user, wrong password, and disabled account; per-IP backoff on
// failures.
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	if req.Username == "" || req.Password == "" {
		respondBadRequest(w, "username and password are required", nil)
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
		debuglog.Warn("userlogin: login failed", "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	token, err := h.sessionMgr.CreateAuthToken(r.Context(), []byte(u.ID.String()), nil)
	if err != nil {
		debuglog.Error("userlogin: session creation failed", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	h.throttle.RecordSuccess(throttleKey)
	if err := h.users.TouchLastLogin(r.Context(), u.ID); err != nil {
		debuglog.Warn("userlogin: failed to record last login", "error", err)
	}
	writeJSON(w, map[string]string{"token": token})
}
