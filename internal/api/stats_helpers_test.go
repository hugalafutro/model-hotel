package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseIncludeLatency_True(t *testing.T) {
	req := httptest.NewRequest("GET", "/?include_latency=true", http.NoBody)
	if !parseIncludeLatency(req) {
		t.Error("Expected true for include_latency=true")
	}
}

func TestParseIncludeLatency_False(t *testing.T) {
	req := httptest.NewRequest("GET", "/?include_latency=false", http.NoBody)
	if parseIncludeLatency(req) {
		t.Error("Expected false for include_latency=false")
	}
}

func TestParseIncludeLatency_Missing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	if parseIncludeLatency(req) {
		t.Error("Expected false when include_latency is missing")
	}
}

func TestParseIncludeLatency_OtherValue(t *testing.T) {
	req := httptest.NewRequest("GET", "/?include_latency=yes", http.NoBody)
	if parseIncludeLatency(req) {
		t.Error("Expected false for include_latency=yes")
	}
}
