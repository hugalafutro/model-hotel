package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// TestParseBearerToken_NoHeaderCoverage tests that a request without Authorization
// header returns ("", false).
func TestParseBearerToken_NoHeaderCoverage(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false when Authorization header is missing")
	}
	if token != "" {
		t.Errorf("Expected empty token, got %q", token)
	}
}

// TestParseBearerToken_WrongSchemeCoverage tests that "Basic abc123" returns ("", false).
func TestParseBearerToken_WrongSchemeCoverage(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Basic abc123")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for Basic auth scheme")
	}
	if token != "" {
		t.Errorf("Expected empty token, got %q", token)
	}
}

// TestParseBearerToken_EmptyTokenCoverage tests that "Bearer " (with space but no token)
// returns ("", false).
func TestParseBearerToken_EmptyTokenCoverage(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer ")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for 'Bearer ' with empty token")
	}
	if token != "" {
		t.Errorf("Expected empty token, got %q", token)
	}
}

// TestParseBearerToken_ValidCoverage tests that "Bearer my-token-123" returns
// ("my-token-123", true).
func TestParseBearerToken_ValidCoverage(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer my-token-123")
	token, ok := ParseBearerToken(r)
	if !ok {
		t.Fatal("ParseBearerToken should return true for valid Bearer header")
	}
	if token != "my-token-123" {
		t.Errorf("Expected token 'my-token-123', got %q", token)
	}
}

// TestParseBearerToken_ShortHeaderCoverage tests that "Bearer" (no space, too short)
// returns ("", false).
func TestParseBearerToken_ShortHeaderCoverage(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for 'Bearer' without space")
	}
	if token != "" {
		t.Errorf("Expected empty token, got %q", token)
	}
}

// TestIsDockerAvailable_SocketNotExist tests that IsDockerAvailable returns
// false when the Docker socket doesn't exist.
func TestIsDockerAvailable_SocketNotExist(t *testing.T) {
	// Save original values
	origSocketPath := dockerSocketPath
	origDockerAvailable := dockerAvailable

	// Reset for test
	dockerCheckMu = sync.Once{}
	dockerAvailable = false

	// Override socket path to non-existent location
	dockerSocketPath = "/nonexistent/docker.sock"

	// Restore original values after test
	defer func() {
		dockerSocketPath = origSocketPath
		dockerCheckMu = sync.Once{}
		dockerAvailable = origDockerAvailable
	}()

	result := IsDockerAvailable()
	if result {
		t.Error("Expected false when Docker socket doesn't exist")
	}
}

// TestSanitizeLogBody_MultibyteTruncationCoverage tests that SanitizeLogBody correctly
// handles multi-byte UTF-8 characters when truncating.
func TestSanitizeLogBody_MultibyteTruncationCoverage(t *testing.T) {
	// String with emoji (4-byte UTF-8 characters)
	body := "Hello 🌍 World 🚀 Test"
	// Truncate to small maxLen that cuts through an emoji
	result := SanitizeLogBody(body, 10)

	// Should be valid UTF-8
	if !utf8.ValidString(result) {
		t.Errorf("Result should be valid UTF-8, got %q", result)
	}

	// Should end with ellipsis
	if !strings.HasSuffix(result, "…") {
		t.Errorf("Result should end with ellipsis, got %q", result)
	}
}

// TestIntToStrCoverage verifies basic integer-to-string conversion.
func TestIntToStrCoverage(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-1, "-1"},
		{999999, "999999"},
		{-12345, "-12345"},
	}
	for _, tc := range tests {
		result := IntToStr(tc.input)
		if result != tc.expected {
			t.Errorf("IntToStr(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
