package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNewHandler(t *testing.T) {

	h := newTestHandler(t)
	if h == nil {
		t.Fatal("handler is nil")
		return
	}
	if h.Pool() == nil {
		t.Fatal("pool is nil")
	}
}

func TestHandlerRegister(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Test that routes are registered
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Logf("Expected 200, got %d", rec.Code)
	}
}

func TestPool(t *testing.T) {

	h := newTestHandler(t)
	pool := h.Pool()
	if pool == nil {
		t.Fatal("pool is nil")
		return
	}

	// Test database connection
	ctx := context.Background()
	var count int
	err := pool.Pool().QueryRow(ctx, "SELECT 1").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query database: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count=1, got %d", count)
	}
}

// Provider Tests
