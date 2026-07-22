package api

import (
	"context"
	"errors"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/config"
)

// stubPwnedChecker is a hand-driven PwnedChecker for policy tests.
type stubPwnedChecker struct {
	breached bool
	count    int
	err      error
	calls    int
}

func (s *stubPwnedChecker) Breached(_ context.Context, _ string) (bool, int, error) {
	s.calls++
	return s.breached, s.count, s.err
}

const (
	shortPassword = "1234567"            // 7 chars, below minPasswordLen
	longPassword  = "longenoughpassword" // >= minPasswordLen
)

func enabledCfg() *config.Config {
	return &config.Config{PwnedPasswordCheckEnabled: true}
}

func TestValidateNewPassword_TooShortSkipsBreachCheck(t *testing.T) {
	chk := &stubPwnedChecker{breached: true}
	h := &Handler{cfg: enabledCfg(), pwnedChecker: chk, settingsRepo: &mockSettingsStore{}}

	if err := h.validateNewPassword(context.Background(), shortPassword); !errors.Is(err, errPasswordTooShort) {
		t.Fatalf("got %v, want errPasswordTooShort", err)
	}
	if chk.calls != 0 {
		t.Fatalf("breach check ran for a too-short password (%d calls); length should short-circuit", chk.calls)
	}
}

func TestValidateNewPassword_RejectsBreached(t *testing.T) {
	chk := &stubPwnedChecker{breached: true, count: 5}
	h := &Handler{cfg: enabledCfg(), pwnedChecker: chk, settingsRepo: &mockSettingsStore{}}

	if err := h.validateNewPassword(context.Background(), longPassword); !errors.Is(err, errPasswordBreached) {
		t.Fatalf("got %v, want errPasswordBreached", err)
	}
}

func TestValidateNewPassword_AllowsClean(t *testing.T) {
	chk := &stubPwnedChecker{breached: false}
	h := &Handler{cfg: enabledCfg(), pwnedChecker: chk, settingsRepo: &mockSettingsStore{}}

	if err := h.validateNewPassword(context.Background(), longPassword); err != nil {
		t.Fatalf("got %v, want nil for a clean password", err)
	}
	if chk.calls != 1 {
		t.Fatalf("breach check ran %d times, want exactly 1", chk.calls)
	}
}

func TestValidateNewPassword_FailsOpenOnCheckerError(t *testing.T) {
	chk := &stubPwnedChecker{err: errors.New("range endpoint unreachable")}
	h := &Handler{cfg: enabledCfg(), pwnedChecker: chk, settingsRepo: &mockSettingsStore{}}

	if err := h.validateNewPassword(context.Background(), longPassword); err != nil {
		t.Fatalf("got %v, want nil (fail open) when the breach service errors", err)
	}
}

func TestValidateNewPassword_EnvKillSwitchSkipsCheck(t *testing.T) {
	chk := &stubPwnedChecker{breached: true}
	h := &Handler{cfg: &config.Config{PwnedPasswordCheckEnabled: false}, pwnedChecker: chk, settingsRepo: &mockSettingsStore{}}

	if err := h.validateNewPassword(context.Background(), longPassword); err != nil {
		t.Fatalf("got %v, want nil when disabled via env", err)
	}
	if chk.calls != 0 {
		t.Fatalf("breach check ran (%d calls) despite the env kill-switch", chk.calls)
	}
}

func TestValidateNewPassword_DBToggleOffSkipsCheck(t *testing.T) {
	chk := &stubPwnedChecker{breached: true}
	settings := &mockSettingsStore{getBoolFn: func(_ context.Context, key string, _ bool) bool {
		return key != settingKeyPwnedPasswordCheck
	}}
	h := &Handler{cfg: enabledCfg(), pwnedChecker: chk, settingsRepo: settings}

	if err := h.validateNewPassword(context.Background(), longPassword); err != nil {
		t.Fatalf("got %v, want nil when disabled via the DB toggle", err)
	}
	if chk.calls != 0 {
		t.Fatalf("breach check ran (%d calls) despite the DB toggle being off", chk.calls)
	}
}

func TestValidateNewPassword_UnwiredCheckerAllows(t *testing.T) {
	h := &Handler{cfg: enabledCfg(), settingsRepo: &mockSettingsStore{}}

	if err := h.validateNewPassword(context.Background(), longPassword); err != nil {
		t.Fatalf("got %v, want nil when no checker is wired", err)
	}
}
