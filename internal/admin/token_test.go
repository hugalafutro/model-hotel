package admin

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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
	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}

	if !strings.HasPrefix(string(data), sha256Prefix) {
		t.Errorf("Stored token should have %q prefix, got %q", sha256Prefix, string(data))
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
	if err := os.WriteFile(tokenPath, []byte(legacyToken), 0o600); err != nil {
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

	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read migrated token file: %v", err)
	}
	if !strings.HasPrefix(string(data), sha256Prefix) {
		t.Errorf("Migrated file should have %q prefix, got %q", sha256Prefix, string(data))
	}
	if len(data) != len(sha256Prefix)+64 {
		t.Errorf("Migrated file should be %d chars (prefix + hash), got %d chars", len(sha256Prefix)+64, len(data))
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
	if err := os.WriteFile(tokenPath, []byte(fakeHash), 0o600); err != nil {
		t.Fatalf("Failed to write hash token: %v", err)
	}

	_, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if isNew {
		t.Error("Existing hash token should not be treated as new")
	}

	data, err := os.ReadFile(tokenPath) //nolint:gosec // test-only: controlled test path
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
	//nolint:gosec // test-only: cleanup before test
	_ = os.Remove(tokenPath)

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
	data, err := os.ReadFile(tokenPath) //nolint:gosec // test-only: controlled test path
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	if string(data) == explicitToken {
		t.Error("Stored file should NOT contain the plaintext token")
	}
	if !strings.HasPrefix(string(data), sha256Prefix) {
		t.Errorf("Stored token should have %q prefix, got %q", sha256Prefix, string(data))
	}
	if len(data) != len(sha256Prefix)+64 {
		t.Errorf("Stored token should be %d chars (prefix + 64-char hash), got %d", len(sha256Prefix)+64, len(data))
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

// --- New tests for sha256: prefix format ---

func TestNewCreatesTokenWithSha256Prefix(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	tokenPath := filepath.Join(tmpDir, "admin-token")
	data, err := os.ReadFile(tokenPath) //nolint:gosec // test-only: controlled test path
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, sha256Prefix) {
		t.Errorf("Newly created token file should start with %q, got %q", sha256Prefix, content)
	}
	if len(content) != len(sha256Prefix)+64 {
		t.Errorf("File content should be %d chars (prefix + 64-char hash), got %d", len(sha256Prefix)+64, len(content))
	}
	if !mgr.Validate(mgr.Token()) {
		t.Error("Token should validate")
	}
}

func TestSha256PrefixedFileReadAsHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a sha256: prefixed hash directly
	hashHex := hex.EncodeToString(make([]byte, 32))
	content := sha256Prefix + hashHex
	tokenPath := filepath.Join(tmpDir, "admin-token")
	if err := os.WriteFile(tokenPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write token file: %v", err)
	}

	mgr, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if isNew {
		t.Error("Prefixed hash file should not be treated as new")
	}

	// File should not be modified (not re-hashed)
	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	if string(data) != content {
		t.Errorf("File should remain unchanged, got %q", string(data))
	}

	// Token() should be empty (no plaintext available)
	if mgr.Token() != "" {
		t.Error("Token() should be empty when loading from stored hash")
	}
}

func TestLegacyBare64HexFileStillWorks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a bare 64-char hex hash (legacy format, no prefix)
	hashHex := hex.EncodeToString(make([]byte, 32))
	tokenPath := filepath.Join(tmpDir, "admin-token")
	if err := os.WriteFile(tokenPath, []byte(hashHex), 0o600); err != nil {
		t.Fatalf("Failed to write token file: %v", err)
	}

	mgr, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if isNew {
		t.Error("Legacy bare hash file should not be treated as new")
	}

	// File should not be modified (backward compat — keep as-is)
	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	if string(data) != hashHex {
		t.Error("Legacy bare hash file should not be rewritten")
	}

	// We can't validate against a known token since we don't know the plaintext,
	// but we can confirm the manager loaded the hash and Token() is empty
	if mgr.Token() != "" {
		t.Error("Token() should be empty when loading from stored hash")
	}
}

func TestValidateTokenWithPrefixedAndLegacyFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a token, get the plaintext
	mgr1, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("First New() failed: %v", err)
	}
	token := mgr1.Token()

	// Read the file — should be sha256: prefixed now
	tokenPath := filepath.Join(tmpDir, "admin-token")
	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	if !strings.HasPrefix(string(data), sha256Prefix) {
		t.Fatal("Expected sha256: prefixed file from new creation")
	}

	// Validate works with sha256: prefixed format
	if !mgr1.Validate(token) {
		t.Error("Validate should work with sha256: prefixed format")
	}

	// Now simulate a legacy bare hash file: write the hash without prefix
	hashPart := string(data[len(sha256Prefix):])
	//nolint:gosec // test-only path
	if err := os.WriteFile(tokenPath, []byte(hashPart), 0o600); err != nil {
		t.Fatalf("Failed to write legacy format: %v", err)
	}

	mgr2, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("Second New() failed: %v", err)
	}

	// Validate still works with legacy bare hash
	if !mgr2.Validate(token) {
		t.Error("Validate should work with legacy bare hash format")
	}
	if mgr2.Validate("wrong-token") {
		t.Error("Wrong token should not validate")
	}
}

func TestPlaintextTokenGetsHashedAndRewrittenWithPrefix(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a non-64-char plaintext token
	plaintext := "my-plaintext-test-token"
	tokenPath := filepath.Join(tmpDir, "admin-token")
	if err := os.WriteFile(tokenPath, []byte(plaintext), 0o600); err != nil {
		t.Fatalf("Failed to write plaintext token: %v", err)
	}

	mgr, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if isNew {
		t.Error("Plaintext token file should not be treated as new")
	}

	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, sha256Prefix) {
		t.Errorf("Rewritten file should have %q prefix, got %q", sha256Prefix, content)
	}
	if len(content) != len(sha256Prefix)+64 {
		t.Errorf("Rewritten file should be %d chars, got %d", len(sha256Prefix)+64, len(content))
	}

	// Token() should be empty (plaintext not stored)
	if mgr.Token() != "" {
		t.Error("Token() should be empty after migration")
	}

	// Validate should work against the original plaintext
	if !mgr.Validate(plaintext) {
		t.Error("Original plaintext should validate after migration")
	}
}

func TestIsNew(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if !mgr.IsNew() {
		t.Error("IsNew() should return true on first creation")
	}

	mgr2, _, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("Second New() failed: %v", err)
	}
	if mgr2.IsNew() {
		t.Error("IsNew() should return false on reload")
	}
}

func TestLoadOrCreateToken_ReadError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a valid sha256: prefixed token file
	tokenPath := filepath.Join(tmpDir, "admin-token")
	hashHex := hex.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(tokenPath, []byte(sha256Prefix+hashHex), 0o600); err != nil {
		t.Fatalf("Failed to write token file: %v", err)
	}

	// Make it unreadable
	if err := os.Chmod(tokenPath, 0o000); err != nil {
		t.Fatalf("Failed to chmod token file: %v", err)
	}
	defer func() {
		// Restore permissions for cleanup
		_ = os.Chmod(tokenPath, 0o600)
	}()

	_, _, err = New(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error when token file is unreadable")
	}
	if !strings.Contains(err.Error(), "failed to read token file") {
		t.Errorf("Error should contain 'failed to read token file', got: %v", err)
	}
}

func TestCreateAndSaveToken_WriteError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory and make it read-only so WriteFile fails
	// This triggers the write error path in createAndSaveToken (line 136-139)
	writeDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(writeDir, 0o500); err != nil {
		t.Fatalf("Failed to create readonly dir: %v", err)
	}
	defer func() {
		// Restore permissions for cleanup
		_ = os.Chmod(writeDir, 0o700)
	}()

	// Use the readonly dir as dataDir - WriteFile will fail with EACCES
	_, _, err = New(writeDir, "test-token")
	if err == nil {
		t.Fatal("Expected error when writing to read-only directory")
	}
	if !strings.Contains(err.Error(), "failed to write token file") {
		t.Errorf("Error should contain 'failed to write token file', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from token_coverage_test.go
// ---------------------------------------------------------------------------

// TestNew_EmptyTokenFile tests that when admin-token file exists but is empty,
// it should be treated as "not found" and create a new token.
func TestNew_EmptyTokenFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "admin-token")

	// Write an empty file
	if err := os.WriteFile(tokenPath, []byte(""), 0o600); err != nil {
		t.Fatalf("Failed to write empty token file: %v", err)
	}

	mgr, isNew, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if !isNew {
		t.Error("Expected isNew=true when token file is empty")
	}

	token := mgr.Token()
	if token == "" {
		t.Error("Token() should return generated plaintext token")
	}
	if len(token) != tokenLength {
		t.Errorf("Token length should be %d, got %d", tokenLength, len(token))
	}
}

// TestNew_MkdirAllFails tests that New returns error when data dir cannot be created.
func TestNew_MkdirAllFails(t *testing.T) {
	// Create a temp file (not a directory)
	tmpFile, err := os.CreateTemp("", "not-a-dir")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFilePath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpFilePath)
	}()

	// Try to create a subdirectory inside the file (should fail)
	subdirPath := filepath.Join(tmpFilePath, "subdir")
	_, _, err = New(subdirPath, "")
	if err == nil {
		t.Fatal("Expected error when data dir cannot be created")
	}
	if !strings.Contains(err.Error(), "failed to create data directory") {
		t.Errorf("Error should contain 'failed to create data directory', got: %v", err)
	}
}

// TestLoadOrCreateToken_PlaintextMigrationWriteFails tests the plaintext migration
// path when WriteFile fails due to permission issues.
func TestLoadOrCreateToken_PlaintextMigrationWriteFails(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "admin-token")

	// Write a plaintext token (not 64 chars, so it triggers migration)
	plaintextToken := "my-plaintext-token"
	if err := os.WriteFile(tokenPath, []byte(plaintextToken), 0o600); err != nil {
		t.Fatalf("Failed to write plaintext token: %v", err)
	}

	// Make the file read-only so the migration write fails
	if err := os.Chmod(tokenPath, 0o400); err != nil {
		t.Fatalf("Failed to make file read-only: %v", err)
	}
	defer func() {
		// Restore permissions for cleanup
		_ = os.Chmod(tokenPath, 0o600)
	}()

	_, _, err := New(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error when migration write fails")
	}
	if !strings.Contains(err.Error(), "failed to migrate token file") {
		t.Errorf("Error should contain 'failed to migrate token file', got: %v", err)
	}
}

// TestGenerateToken_DeterministicLength verifies that generateToken always
// returns exactly tokenLength (32) characters.
func TestGenerateToken_DeterministicLength(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := newManagerForTest(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Call generateToken multiple times and verify length
	for i := 0; i < 10; i++ {
		token, err := mgr.generateToken()
		if err != nil {
			t.Fatalf("generateToken() failed: %v", err)
		}
		if len(token) != tokenLength {
			t.Errorf("generateToken() iteration %d: length should be %d, got %d", i, tokenLength, len(token))
		}
	}
}

// newManagerForTest creates a Manager without calling New() to allow
// direct testing of unexported methods.
func newManagerForTest(dataDir string) (*Manager, error) {
	return &Manager{dataDir: dataDir}, nil
}
