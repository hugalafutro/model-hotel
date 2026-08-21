package adminauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// expiredCeremonySession stores a ceremony session whose expires_at is already
// in the past, simulating a challenge issued longer ago than the ceremony TTL.
func expiredCeremonySession(t *testing.T, repo *webauthn.Repository, sessionType string) uuid.UUID {
	t.Helper()
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"challenge":"test-challenge"}`),
		Type:        sessionType,
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(-1 * time.Minute),
	}
	if err := repo.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		repo.DeleteSession(context.Background(), sessionID)
	})
	return sessionID
}

func expiryTestRepo(t *testing.T) *webauthn.Repository {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)
	return webauthn.NewRepository(pool)
}

// TestWebAuthnHandler_RegisterFinish_ExpiredSession tests that a registration
// ceremony session past its expires_at is rejected instead of waiting for the
// hourly cleanup sweep to remove it.
func TestWebAuthnHandler_RegisterFinish_ExpiredSession(t *testing.T) {
	repo := expiryTestRepo(t)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	sessionID := expiredCeremonySession(t, repo, "registration")

	body := `{"session_id": "` + sessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "session expired") {
		t.Errorf("expected 'session expired' error, got: %s", w.Body.String())
	}
	// The expired session must be consumed, not left claimable.
	if _, err := repo.GetSession(context.Background(), sessionID); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("expected expired session to be deleted, GetSession err = %v", err)
	}
}

// TestWebAuthnHandler_LoginFinish_ExpiredSession tests that a login ceremony
// session past its expires_at is rejected instead of waiting for the hourly
// cleanup sweep to remove it.
func TestWebAuthnHandler_LoginFinish_ExpiredSession(t *testing.T) {
	repo := expiryTestRepo(t)
	h := newTestWebAuthnHandler(repo, nil, nil, nil)

	sessionID := expiredCeremonySession(t, repo, "login")

	body := `{"session_id": "` + sessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "session expired") {
		t.Errorf("expected 'session expired' error, got: %s", w.Body.String())
	}
	// The expired session must be consumed, not left claimable.
	if _, err := repo.GetSession(context.Background(), sessionID); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("expected expired session to be deleted, GetSession err = %v", err)
	}
}
