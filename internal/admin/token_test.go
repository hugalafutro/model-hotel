package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if mgr.Token() == "" {
		t.Error("Token should not be empty")
	}
}

func TestTokenPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr1, err := New(tmpDir)
	if err != nil {
		t.Fatalf("First New() failed: %v", err)
	}

	token1 := mgr1.Token()

	mgr2, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Second New() failed: %v", err)
	}

	token2 := mgr2.Token()

	if token1 != token2 {
		t.Errorf("Tokens should persist. Expected %q, got %q", token1, token2)
	}
}

func TestTokenValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := New(tmpDir)
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
}

func TestTokenFileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "llm-proxy-admin-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	tokenPath := filepath.Join(tmpDir, "admin-token")
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		t.Error("Token file should exist")
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("Failed to read token file: %v", err)
	}

	if string(data) != mgr.Token() {
		t.Error("Token file content should match manager token")
	}
}
