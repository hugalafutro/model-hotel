package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

func TestExtractKey_VirtualKeyContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyHashKey, "abc123hash"))
	req.RemoteAddr = "1.2.3.4:5678"

	key := extractKey(req)
	if key != "abc123hash" {
		t.Errorf("Expected virtual key hash, got %q", key)
	}
}

func TestExtractKey_FallbackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.RemoteAddr = "1.2.3.4:5678"

	key := extractKey(req)
	if key != "1.2.3.4:5678" {
		t.Errorf("Expected remote addr, got %q", key)
	}
}

func TestExtractKey_EmptyVirtualKeyFallsBack(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyHashKey, ""))
	req.RemoteAddr = "1.2.3.4:5678"

	key := extractKey(req)
	if key != "1.2.3.4:5678" {
		t.Errorf("Expected remote addr fallback for empty virtual key, got %q", key)
	}
}

func TestExtractKey_WrongTypeFallsBack(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyHashKey, 12345))
	req.RemoteAddr = "1.2.3.4:5678"

	key := extractKey(req)
	if key != "1.2.3.4:5678" {
		t.Errorf("Expected remote addr fallback for wrong type, got %q", key)
	}
}
