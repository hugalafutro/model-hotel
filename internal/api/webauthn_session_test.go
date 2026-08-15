package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// These tests cover Handler.SetWebAuthnSessionManager, an api.Handler concern.
// They stayed in package api when the WebAuthn/TOTP HTTP handlers moved to
// internal/adminauth (the handler tests moved with them).

// mockWebAuthnSessionMgr implements WebAuthnSessionManager for testing.
type mockWebAuthnSessionMgr struct {
	validateFn     func(ctx context.Context, token string) bool
	revokeFn       func(ctx context.Context, token string) bool
	createFn       func(ctx context.Context, userID, credentialID []byte) (string, error)
	revokeOthersFn func(ctx context.Context, identity []byte, candidateTokens ...string) (int64, error)
	listFn         func(ctx context.Context, identity []byte, candidateTokens ...string) ([]webauthn.AuthSessionInfo, error)
	revokeByIDFn   func(ctx context.Context, identity []byte, id uuid.UUID, candidateTokens ...string) error
	authFn         func(ctx context.Context, token string) (webauthn.AuthResult, bool)
	// Verify invocations, so a test can prove a re-check did not slide. Atomic:
	// the SSE reauth goroutine keeps ticking until its context ends, which can
	// be after the stream handler returned and the test reads the count.
	verifyCalls atomic.Int32
}

// RevokeOtherSessions defers to revokeOthersFn when set, so a test can assert
// what the handler passed it; otherwise it reports that nothing was revoked.
func (m *mockWebAuthnSessionMgr) RevokeOtherSessions(ctx context.Context, identity []byte, candidateTokens ...string) (int64, error) {
	if m.revokeOthersFn != nil {
		return m.revokeOthersFn(ctx, identity, candidateTokens...)
	}
	return 0, nil
}

// CreateAuthToken mints a session token, deferring to createFn when set so
// tests can return a deterministic token (or an error) without a session store.
// The device meta is dropped: none of these tests read it back.
func (m *mockWebAuthnSessionMgr) CreateAuthToken(ctx context.Context, userID, credentialID []byte, _ webauthn.SessionMeta) (string, error) {
	if m.createFn != nil {
		return m.createFn(ctx, userID, credentialID)
	}
	return "mock-session-token", nil
}

// ListAuthSessions defers to listFn when set; otherwise an empty list.
func (m *mockWebAuthnSessionMgr) ListAuthSessions(ctx context.Context, identity []byte, candidateTokens ...string) ([]webauthn.AuthSessionInfo, error) {
	if m.listFn != nil {
		return m.listFn(ctx, identity, candidateTokens...)
	}
	return nil, nil
}

// RevokeSessionByID defers to revokeByIDFn when set; otherwise not-found,
// matching a store with no sessions.
func (m *mockWebAuthnSessionMgr) RevokeSessionByID(ctx context.Context, identity []byte, id uuid.UUID, candidateTokens ...string) error {
	if m.revokeByIDFn != nil {
		return m.revokeByIDFn(ctx, identity, id, candidateTokens...)
	}
	return webauthn.ErrNotFound
}

func (m *mockWebAuthnSessionMgr) Validate(ctx context.Context, token string) bool {
	if m.validateFn != nil {
		return m.validateFn(ctx, token)
	}
	return false
}

// TokenUser mirrors Validate and reports the legacy admin handle, matching
// what pre-multi-user sessions carry.
func (m *mockWebAuthnSessionMgr) TokenUser(ctx context.Context, token string) ([]byte, bool) {
	if m.Validate(ctx, token) {
		return []byte("admin"), true
	}
	return nil, false
}

// Authenticate mirrors TokenUser; authFn, when set, decides the expiry and
// whether the call counts as having slid it (so middleware tests can drive the
// cookie re-issue branch without a real store).
func (m *mockWebAuthnSessionMgr) Authenticate(ctx context.Context, token string) (webauthn.AuthResult, bool) {
	if m.authFn != nil {
		return m.authFn(ctx, token)
	}
	uid, ok := m.TokenUser(ctx, token)
	if !ok {
		return webauthn.AuthResult{}, false
	}
	return webauthn.AuthResult{UserID: uid, ExpiresAt: time.Now().Add(webauthn.AuthTokenTTL)}, true
}

// Verify never slides: it answers like TokenUser with a plain expiry and counts
// the call.
func (m *mockWebAuthnSessionMgr) Verify(ctx context.Context, token string) (webauthn.AuthResult, bool) {
	m.verifyCalls.Add(1)
	uid, ok := m.TokenUser(ctx, token)
	if !ok {
		return webauthn.AuthResult{}, false
	}
	return webauthn.AuthResult{UserID: uid, ExpiresAt: time.Now().Add(webauthn.AuthTokenTTL)}, true
}

func (m *mockWebAuthnSessionMgr) RevokeAuthToken(ctx context.Context, token string) bool {
	if m.revokeFn != nil {
		return m.revokeFn(ctx, token)
	}
	return false
}

// TestSetWebAuthnSessionManager_SetsField verifies the session manager is wired
// onto the Handler and validates through the interface.
func TestSetWebAuthnSessionManager_SetsField(t *testing.T) {
	h := newTestHandler(t)

	if h.webauthnSessionMgr != nil {
		t.Error("expected nil webauthnSessionMgr before SetWebAuthnSessionManager")
	}

	mockMgr := &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, _ string) bool { return true },
		revokeFn:   func(_ context.Context, _ string) bool { return true },
	}
	h.SetWebAuthnSessionManager(mockMgr)

	if h.webauthnSessionMgr == nil {
		t.Error("expected non-nil webauthnSessionMgr after SetWebAuthnSessionManager")
	}
	if !h.webauthnSessionMgr.Validate(context.Background(), "any-token") {
		t.Error("expected Validate to return true via mock")
	}
}

// TestSetWebAuthnSessionManager_NilArg verifies the field can be cleared.
func TestSetWebAuthnSessionManager_NilArg(t *testing.T) {
	h := newTestHandler(t)

	mockMgr := &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, _ string) bool { return true },
		revokeFn:   func(_ context.Context, _ string) bool { return true },
	}
	h.SetWebAuthnSessionManager(mockMgr)
	if h.webauthnSessionMgr == nil {
		t.Fatal("expected non-nil after set")
	}

	h.SetWebAuthnSessionManager(nil)
	if h.webauthnSessionMgr != nil {
		t.Error("expected nil webauthnSessionMgr after SetWebAuthnSessionManager(nil)")
	}
}
