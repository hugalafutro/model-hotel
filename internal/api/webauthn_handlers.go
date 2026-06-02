package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnx "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// WebAuthnHandler handles WebAuthn/FIDO2 registration and login endpoints.
type WebAuthnHandler struct {
	webauthnRepo *webauthn.Repository
	relyingParty *webauthnx.WebAuthn
	sessionMgr   *webauthn.SessionManager
	adminMgr     AdminAuthenticator
	ipLimiter    IPLimiterMiddleware
}

// IPLimiterMiddleware is the interface for IP rate limiting middleware.
type IPLimiterMiddleware interface {
	Middleware(next http.Handler) http.Handler
}

// NewWebAuthnHandler creates a new WebAuthn handler with the given dependencies.
func NewWebAuthnHandler(
	webauthnRepo *webauthn.Repository,
	relyingParty *webauthnx.WebAuthn,
	sessionMgr *webauthn.SessionManager,
	adminMgr AdminAuthenticator,
	ipLimiter IPLimiterMiddleware,
) *WebAuthnHandler {
	return &WebAuthnHandler{
		webauthnRepo: webauthnRepo,
		relyingParty: relyingParty,
		sessionMgr:   sessionMgr,
		adminMgr:     adminMgr,
		ipLimiter:    ipLimiter,
	}
}

// Available reports whether WebAuthn is enabled on the server.
func (h *WebAuthnHandler) Available(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]bool{"enabled": h.relyingParty != nil})
}

// Register mounts WebAuthn routes on the given router.
// Registration and credential management require admin auth.
// Login endpoints are public (called from the login screen).
func (h *WebAuthnHandler) Register(r chi.Router) {
	r.Route("/webauthn", func(r chi.Router) {
		r.Get("/available", h.Available)
		r.Group(func(r chi.Router) {
			r.Use(h.adminOrSessionAuth)
			r.Post("/register/start", h.RegisterStart)
			r.Post("/register/finish", h.RegisterFinish)
			r.Get("/credentials", h.ListCredentials)
			r.Delete("/credentials/{id}", h.DeleteCredential)
			r.Patch("/credentials/{id}", h.RenameCredential)
			r.Post("/logout", h.Logout)
		})
		r.Group(func(r chi.Router) {
			r.Use(h.ipLimiter.Middleware)
			r.Post("/login/start", h.LoginStart)
			r.Post("/login/finish", h.LoginFinish)
		})
	})
}

// adminOrSessionAuth validates either the admin token or a WebAuthn session
// token for WebAuthn management routes. This allows passkey-authenticated
// sessions to manage their own credentials.
func (h *WebAuthnHandler) adminOrSessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := util.ParseBearerToken(r)
		if !ok {
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		if h.adminMgr.Validate(token) {
			next.ServeHTTP(w, r)
			return
		}

		if h.sessionMgr.Validate(r.Context(), token) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Invalid admin token or session token", http.StatusUnauthorized)
	})
}

// sessionTTL is the time-to-live for WebAuthn registration/login sessions.
const sessionTTL = 5 * time.Minute

// RegisterStart begins a WebAuthn credential registration ceremony.
// POST /webauthn/register/start (admin auth required)
func (h *WebAuthnHandler) RegisterStart(w http.ResponseWriter, r *http.Request) {
	creds, err := h.webauthnRepo.ListCredentials(r.Context())
	if err != nil {
		debuglog.Error("webauthn: failed to list credentials for registration", "error", err)
		respondError(w, "failed to list credentials", err, http.StatusInternalServerError)
		return
	}

	adminUser := webauthn.NewAdminUser()
	webauthnCreds := make([]webauthnx.Credential, len(creds))
	for i, c := range creds {
		webauthnCreds[i] = c.ToWebAuthnCredential()
	}
	adminUser.SetCredentials(webauthnCreds)

	creation, session, err := h.relyingParty.BeginRegistration(
		adminUser,
		webauthnx.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthnx.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		}),
	)
	if err != nil {
		debuglog.Error("webauthn: BeginRegistration failed", "error", err)
		respondError(w, "failed to begin registration", err, http.StatusInternalServerError)
		return
	}

	sessionJSON, err := json.Marshal(session)
	if err != nil {
		debuglog.Error("webauthn: failed to marshal session data", "error", err)
		respondError(w, "failed to marshal session data", err, http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New()
	sessionRec := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   session.Challenge,
		SessionData: sessionJSON,
		Type:        "registration",
		UserID:      adminUser.WebAuthnID(),
		ExpiresAt:   time.Now().Add(sessionTTL),
	}

	if err := h.webauthnRepo.CreateSession(r.Context(), sessionRec); err != nil {
		debuglog.Error("webauthn: failed to create registration session", "session_id", sessionID, "error", err)
		respondError(w, "failed to create session", err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"session_id": sessionID.String(),
		"options":    creation.Response,
	})
}

// registerFinishRequest is the request body for completing a registration.
type registerFinishRequest struct {
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential"`
}

// RegisterFinish completes a WebAuthn credential registration ceremony.
// POST /webauthn/register/finish (admin auth required)
func (h *WebAuthnHandler) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	var req registerFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil {
		respondBadRequest(w, "invalid session_id", err)
		return
	}

	sessionRec, err := h.webauthnRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		debuglog.Error("webauthn: failed to load registration session", "session_id", sessionID, "error", err)
		respondError(w, "session not found", err, http.StatusBadRequest)
		return
	}
	if sessionRec.Type != "registration" {
		http.Error(w, "invalid session type", http.StatusBadRequest)
		return
	}

	if err := h.webauthnRepo.DeleteSession(r.Context(), sessionID); err != nil {
		debuglog.Info("webauthn: failed to delete registration session", "session_id", sessionID, "error", err)
	}

	var session webauthnx.SessionData
	if err := json.Unmarshal(sessionRec.SessionData, &session); err != nil {
		debuglog.Error("webauthn: failed to unmarshal session data", "session_id", sessionID, "error", err)
		respondError(w, "invalid session data", err, http.StatusInternalServerError)
		return
	}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(
		io.NopCloser(bytes.NewReader(req.Credential)),
	)
	if err != nil {
		debuglog.Error("webauthn: failed to parse credential creation response", "error", err)
		respondBadRequest(w, "invalid credential response", err)
		return
	}

	creds, err := h.webauthnRepo.ListCredentials(r.Context())
	if err != nil {
		debuglog.Error("webauthn: failed to list credentials for registration finish", "error", err)
		respondError(w, "failed to list credentials", err, http.StatusInternalServerError)
		return
	}

	adminUser := webauthn.NewAdminUser()
	webauthnCreds := make([]webauthnx.Credential, len(creds))
	for i, c := range creds {
		webauthnCreds[i] = c.ToWebAuthnCredential()
	}
	adminUser.SetCredentials(webauthnCreds)

	credential, err := h.relyingParty.CreateCredential(adminUser, session, parsedResponse)
	if err != nil {
		debuglog.Error("webauthn: CreateCredential failed", "error", err)
		respondBadRequest(w, "credential verification failed", err)
		return
	}

	credRec := webauthn.FromWebAuthnCredential(credential)
	if err := h.webauthnRepo.StoreCredential(r.Context(), credRec); err != nil {
		debuglog.Error("webauthn: failed to store credential", "error", err)
		respondError(w, "failed to store credential", err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("webauthn: credential registered successfully")
	events.Publish(events.Event{
		Type:     "webauthn.credential_registered",
		Severity: "success",
		Source:   "webauthn",
		Message:  "Passkey registered successfully",
	})
	writeJSON(w, map[string]interface{}{
		"success": true,
	})
}

// LoginStart begins a WebAuthn discoverable login ceremony.
// POST /webauthn/login/start (no auth required)
func (h *WebAuthnHandler) LoginStart(w http.ResponseWriter, r *http.Request) {
	assertion, session, err := h.relyingParty.BeginDiscoverableLogin()
	if err != nil {
		debuglog.Error("webauthn: BeginDiscoverableLogin failed", "error", err)
		respondError(w, "failed to begin login", err, http.StatusInternalServerError)
		return
	}

	sessionJSON, err := json.Marshal(session)
	if err != nil {
		debuglog.Error("webauthn: failed to marshal login session data", "error", err)
		respondError(w, "failed to marshal session data", err, http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New()
	sessionRec := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   session.Challenge,
		SessionData: sessionJSON,
		Type:        "login",
		ExpiresAt:   time.Now().Add(sessionTTL),
	}

	if err := h.webauthnRepo.CreateSession(r.Context(), sessionRec); err != nil {
		debuglog.Error("webauthn: failed to create login session", "session_id", sessionID, "error", err)
		respondError(w, "failed to create session", err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"session_id": sessionID.String(),
		"options":    assertion.Response,
	})
}

// loginFinishRequest is the request body for completing a login.
type loginFinishRequest struct {
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential"`
}

// LoginFinish completes a WebAuthn discoverable login ceremony.
// POST /webauthn/login/finish (no auth required)
func (h *WebAuthnHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
	var req loginFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil {
		respondBadRequest(w, "invalid session_id", err)
		return
	}

	sessionRec, err := h.webauthnRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		debuglog.Error("webauthn: failed to load login session", "session_id", sessionID, "error", err)
		respondError(w, "session not found", err, http.StatusBadRequest)
		return
	}
	if sessionRec.Type != "login" {
		http.Error(w, "invalid session type", http.StatusBadRequest)
		return
	}

	if err := h.webauthnRepo.DeleteSession(r.Context(), sessionID); err != nil {
		debuglog.Info("webauthn: failed to delete login session", "session_id", sessionID, "error", err)
	}

	var session webauthnx.SessionData
	if err := json.Unmarshal(sessionRec.SessionData, &session); err != nil {
		debuglog.Error("webauthn: failed to unmarshal login session data", "session_id", sessionID, "error", err)
		respondError(w, "invalid session data", err, http.StatusInternalServerError)
		return
	}

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(
		io.NopCloser(bytes.NewReader(req.Credential)),
	)
	if err != nil {
		debuglog.Error("webauthn: failed to parse credential request response", "error", err)
		respondBadRequest(w, "invalid credential response", err)
		return
	}

	userLookup := func(rawID, userHandle []byte) (webauthnx.User, error) {
		cred, err := h.webauthnRepo.GetCredentialByID(r.Context(), rawID)
		if err != nil {
			return nil, err
		}
		adminUser := webauthn.NewAdminUser()
		adminUser.SetCredentials([]webauthnx.Credential{cred.ToWebAuthnCredential()})
		return adminUser, nil
	}

	user, parsedCred, err := h.relyingParty.ValidatePasskeyLogin(userLookup, session, parsedResponse)
	if err != nil {
		debuglog.Error("webauthn: ValidatePasskeyLogin failed", "error", err)
		respondBadRequest(w, "passkey login verification failed", err)
		return
	}

	if err := h.webauthnRepo.UpdateSignCount(r.Context(), parsedCred.ID, parsedCred.Authenticator.SignCount); err != nil {
		debuglog.Error("webauthn: failed to update sign count", "error", err)
		respondError(w, "failed to update credential", err, http.StatusInternalServerError)
		return
	}

	token, err := h.sessionMgr.CreateAuthToken(r.Context(), user.WebAuthnID(), parsedCred.ID)
	if err != nil {
		debuglog.Error("webauthn: failed to create auth token", "error", err)
		respondError(w, "failed to create auth token", err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("webauthn: passkey login successful")
	writeJSON(w, map[string]interface{}{
		"token": token,
	})
}

// Logout revokes a WebAuthn session token.
// POST /webauthn/logout (admin or session auth required)
func (h *WebAuthnHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token, ok := util.ParseBearerToken(r)
	if !ok {
		http.Error(w, "Authorization header required", http.StatusUnauthorized)
		return
	}

	h.sessionMgr.RevokeAuthToken(r.Context(), token)

	writeJSON(w, map[string]interface{}{
		"success": true,
	})
}

// credentialResponse is the API-friendly representation of a WebAuthn credential
// returned by the ListCredentials endpoint. The ID is base64url-encoded so it can
// be used directly in the DELETE /webauthn/credentials/{id} URL path.
type credentialResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Transport []string `json:"transports"`
	CreatedAt string   `json:"created_at"`
	AAGUID    string   `json:"aaguid"`
	SignCount uint32   `json:"sign_count"`
}

// ListCredentials returns all registered WebAuthn credentials.
// GET /webauthn/credentials (admin auth required)
func (h *WebAuthnHandler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	creds, err := h.webauthnRepo.ListCredentials(r.Context())
	if err != nil {
		debuglog.Error("webauthn: failed to list credentials", "error", err)
		respondError(w, "failed to list credentials", err, http.StatusInternalServerError)
		return
	}

	resp := make([]credentialResponse, len(creds))
	for i, c := range creds {
		resp[i] = credentialResponse{
			ID:        base64.RawURLEncoding.EncodeToString(c.ID),
			Name:      c.Name,
			Transport: c.Transport,
			CreatedAt: c.CreatedAt.Format(time.RFC3339),
			AAGUID:    c.AAGUID.String(),
			SignCount: c.SignCount,
		}
	}

	writeJSON(w, resp)
}

// DeleteCredential deletes a WebAuthn credential by its base64url-encoded ID.
// DELETE /webauthn/credentials/{id} (admin auth required)
func (h *WebAuthnHandler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "id")
	if rawID == "" {
		http.Error(w, "credential ID is required", http.StatusBadRequest)
		return
	}

	credID, err := base64.RawURLEncoding.DecodeString(rawID)
	if err != nil {
		debuglog.Info("webauthn: invalid credential ID encoding", "raw_id", rawID, "error", err)
		respondBadRequest(w, "invalid credential ID", err)
		return
	}

	if err := h.webauthnRepo.DeleteCredential(r.Context(), credID); err != nil {
		debuglog.Error("webauthn: failed to delete credential", "cred_id", rawID, "error", err)
		respondError(w, "failed to delete credential", err, http.StatusInternalServerError)
		return
	}

	events.Publish(events.Event{
		Type:     "webauthn.credential_deleted",
		Severity: "info",
		Source:   "webauthn",
		Message:  "Passkey deleted",
	})
	w.WriteHeader(http.StatusNoContent)
}

type renameCredentialRequest struct {
	Name string `json:"name"`
}

// RenameCredential updates the display name of a passkey.
func (h *WebAuthnHandler) RenameCredential(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "id")
	if rawID == "" {
		http.Error(w, "credential ID is required", http.StatusBadRequest)
		return
	}

	credID, err := base64.RawURLEncoding.DecodeString(rawID)
	if err != nil {
		respondBadRequest(w, "invalid credential ID", err)
		return
	}

	var req renameCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	trimmed := strings.TrimSpace(req.Name)
	if trimmed == "" || len(trimmed) > 128 {
		http.Error(w, "name must be 1-128 characters", http.StatusBadRequest)
		return
	}

	if err := h.webauthnRepo.RenameCredential(r.Context(), credID, trimmed); err != nil {
		debuglog.Error("webauthn: failed to rename credential", "cred_id", rawID, "error", err)
		respondError(w, "failed to rename credential", err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"success": true})
}
