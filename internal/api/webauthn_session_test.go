package api

import (
	"context"
	"testing"
)

// These tests cover Handler.SetWebAuthnSessionManager, an api.Handler concern.
// They stayed in package api when the WebAuthn/TOTP HTTP handlers moved to
// internal/adminauth (the handler tests moved with them).

// mockWebAuthnSessionMgr implements WebAuthnSessionManager for testing.
type mockWebAuthnSessionMgr struct {
	validateFn func(ctx context.Context, token string) bool
	revokeFn   func(ctx context.Context, token string) bool
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
