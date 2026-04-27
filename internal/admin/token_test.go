package admin

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCreatesHashedToken(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if !isNew {
		t.Error("Expected isNew=true on first creation")
	}

	token := mgr.Token()
	if token == "" {
		t.Error("Token() should return plaintext on first creation")
	}
	if len(token) != tokenLength {
		t.Errorf("Token length should be %d, got %d", tokenLength, len(token))
	}

	tokenPath := filepath.Join(tmpDir, "admin-token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}

	if len(data) != 64 {
		t.Errorf("Stored token should be 64-char SHA-256 hash, got %d chars", len(data))
	}
	if string(data) == token {
		t.Error("Stored file should NOT contain the plaintext token")
	}
}

func TestValidationWithHashedToken(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	token := mgr.Token()

	if !mgr.Validate(token) {
		t.Error("Valid token should pass validation")
	}

	if mgr.Validate("wrong-token") {
		t.Error("Invalid token should fail validation")
	}

	if mgr.Validate("") {
		t.Error("Empty token should fail validation")
	}
}

func TestTokenNotShownOnReload(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, _, err = New(tmpDir, "")
	if err != nil {
		t.Fatalf("First New() failed: %v", err)
	}

	mgr2, isNew2, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("Second New() failed: %v", err)
	}

	if isNew2 {
		t.Error("Expected isNew=false on reload")
	}

	if mgr2.Token() != "" {
		t.Error("Token() should return empty string on reload (plaintext not available)")
	}
}

func TestValidationPersistsAcrossReload(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr1, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("First New() failed: %v", err)
	}
	token := mgr1.Token()

	mgr2, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("Second New() failed: %v", err)
	}

	if !mgr2.Validate(token) {
		t.Error("Token should still validate after reload")
	}
}

func TestLegacyPlaintextMigration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	legacyToken := "abc123def456ghi789jkl012mno345pq"
	tokenPath := filepath.Join(tmpDir, "admin-token")
	if err := os.WriteFile(tokenPath, []byte(legacyToken), 0600); err != nil {
		t.Fatalf("Failed to write legacy token: %v", err)
	}

	mgr, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if isNew {
		t.Error("Legacy token should not be treated as new")
	}

	if !mgr.Validate(legacyToken) {
		t.Error("Legacy token should still validate after migration")
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read migrated token file: %v", err)
	}
	if len(data) != 64 {
		t.Errorf("Migrated file should be 64-char hash, got %d chars", len(data))
	}

	mgr2, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("Reload after migration failed: %v", err)
	}
	if !mgr2.Validate(legacyToken) {
		t.Error("Legacy token should still validate after reload post-migration")
	}
}

func TestExistingHashTokenNotMigrated(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fakeHash := hex.EncodeToString(make([]byte, 32))
	tokenPath := filepath.Join(tmpDir, "admin-token")
	if err := os.WriteFile(tokenPath, []byte(fakeHash), 0600); err != nil {
		t.Fatalf("Failed to write hash token: %v", err)
	}

	_, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if isNew {
		t.Error("Existing hash token should not be treated as new")
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	if string(data) != fakeHash {
		t.Error("Existing hash token should not be modified")
	}
}

func TestRegenerationByDeletingFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr1, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("First New() failed: %v", err)
	}
	oldToken := mgr1.Token()

	tokenPath := filepath.Join(tmpDir, "admin-token")
	os.Remove(tokenPath)

	mgr2, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("Second New() failed: %v", err)
	}

	if !isNew {
		t.Error("Should be new after deleting token file")
	}

	newToken := mgr2.Token()
	if newToken == oldToken {
		t.Error("New token should differ from old token")
	}

	if mgr2.Validate(oldToken) {
		t.Error("Old token should not validate after regeneration")
	}
	if !mgr2.Validate(newToken) {
		t.Error("New token should validate")
	}
}

func TestNewWithExplicitToken(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	explicitToken := "my-explicit-admin-token-value"
	mgr, isNew, err := New(tmpDir, explicitToken)
	if err != nil {
		t.Fatalf("New() with explicit token failed: %v", err)
	}

	if !isNew {
		t.Error("Expected isNew=true when creating with explicit token")
	}

	if mgr.Token() != explicitToken {
		t.Errorf("Token() should return the explicit token, got %q", mgr.Token())
	}

	if !mgr.Validate(explicitToken) {
		t.Error("Explicit token should pass validation")
	}

	// Verify the file stores the hash, not the plaintext
	tokenPath := filepath.Join(tmpDir, "admin-token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	if string(data) == explicitToken {
		t.Error("Stored file should NOT contain the plaintext token")
	}
	if len(data) != 64 {
		t.Errorf("Stored token should be 64-char SHA-256 hash, got %d chars", len(data))
	}

	// Verify the token persists across reload (and explicit token is ignored on reload)
	mgr2, isNew2, err := New(tmpDir, "some-other-token")
	if err != nil {
		t.Fatalf("Reload New() failed: %v", err)
	}

	if isNew2 {
		t.Error("Should not be new on reload")
	}

	if !mgr2.Validate(explicitToken) {
		t.Error("Explicit token should still validate after reload")
	}

	if mgr2.Validate("some-other-token") {
		t.Error("Ignored explicit token on reload should NOT validate")
	}
}
