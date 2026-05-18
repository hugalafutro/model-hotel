package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
