package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func changePassword(t *testing.T, r chi.Router, token, current, next string) int {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/auth/password", token,
		fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, current, next))
	return w.Code
}

func TestChangeOwnPassword_Success(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	uid, token := userSession(t, r, sm, "pw-user")

	if code := changePassword(t, r, token, "password123", "password456"); code != http.StatusOK {
		t.Fatalf("change: %d, want 200", code)
	}
	// Every session was revoked, the one that made the change included.
	if w := doJSON(t, r, http.MethodGet, "/auth/me", token, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("old session after change: %d, want 401", w.Code)
	}
	// The new password is live: a fresh login round-trip through the user
	// login handler is covered in adminauth; here we assert the hash changed
	// by re-running the change with old and new current passwords.
	token2, err := sm.CreateAuthToken(context.Background(), []byte(uid), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if code := changePassword(t, r, token2, "password123", "password789"); code != http.StatusUnauthorized {
		t.Errorf("old current password accepted: %d, want 401", code)
	}
	if code := changePassword(t, r, token2, "password456", "password789"); code != http.StatusOK {
		t.Errorf("new current password rejected: %d, want 200", code)
	}
}

func TestChangeOwnPassword_Validation(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	_, token := userSession(t, r, sm, "pw-val")

	// Short new password refused before anything is checked.
	if code := changePassword(t, r, token, "password123", "short"); code != http.StatusBadRequest {
		t.Errorf("short password: %d, want 400", code)
	}
	// Env-token admin has no users row.
	if code := changePassword(t, r, envAdminToken, "x", "password456"); code != http.StatusBadRequest {
		t.Errorf("env admin: %d, want 400", code)
	}
	// Wrong current password is a 401 and does not change anything.
	if code := changePassword(t, r, token, "nope-nope-nope", "password456"); code != http.StatusUnauthorized {
		t.Errorf("wrong current: %d, want 401", code)
	}
	if w := doJSON(t, r, http.MethodGet, "/auth/me", token, ""); w.Code != http.StatusOK {
		t.Errorf("session revoked on failed change: %d, want 200", w.Code)
	}
}

func TestChangeOwnPassword_ThrottlesGuessing(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	_, token := userSession(t, r, sm, "pw-throttle")

	// Burn through the free failures; the throttle then answers 429 before
	// the password is even checked.
	var got429 bool
	for range 8 {
		code := changePassword(t, r, token, "wrong-password", "password456")
		if code == http.StatusTooManyRequests {
			got429 = true
			break
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("unexpected status %d", code)
		}
	}
	if !got429 {
		t.Fatal("throttle never engaged after repeated failures")
	}
}
