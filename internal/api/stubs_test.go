package api

import (
	"context"
	"testing"
)

// stubTotpStatus is a configurable TotpStatus for AuthMiddleware tests. It was
// shared with the TOTP handler tests before those moved to internal/adminauth;
// admin_test.go still uses it to exercise Handler.AuthMiddleware's TOTP gate.
type stubTotpStatus struct {
	enabled bool
	err     error
}

func (s *stubTotpStatus) IsEnabled(context.Context) (bool, error) {
	return s.enabled, s.err
}

// truncateTOTPTables clears TOTP state between AuthMiddleware tests.
func truncateTOTPTables(t *testing.T) {
	t.Helper()
	if apiTestDB == nil {
		t.Fatal("test database not available")
	}
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`TRUNCATE admin_totp, admin_totp_recovery`); err != nil {
		t.Fatalf("failed to truncate totp tables: %v", err)
	}
}
