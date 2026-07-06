package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// doDiscoveryRequest — retry behavior for discovery fetches. All services are
// built with retryBaseDelay left at its zero value so backoffs are instant.
// ---------------------------------------------------------------------------

func retryTestService(server *httptest.Server) *DiscoveryService {
	return &DiscoveryService{httpClient: server.Client()}
}

func TestDoDiscoveryRequest_RetriesRetryableStatusThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "upstream hiccup", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	d := retryTestService(server)
	resp, err := d.doDiscoveryRequest(context.Background(), func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), "GET", server.URL, http.NoBody)
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestDoDiscoveryRequest_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	d := retryTestService(server)
	_, err := d.doDiscoveryRequest(context.Background(), func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), "GET", server.URL, http.NoBody)
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := calls.Load(); got != maxDiscoveryRetries {
		t.Errorf("expected %d attempts, got %d", maxDiscoveryRetries, got)
	}
}

func TestDoDiscoveryRequest_NonRetryableStatusReturnedImmediately(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	d := retryTestService(server)
	resp, err := d.doDiscoveryRequest(context.Background(), func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), "GET", server.URL, http.NoBody)
	})
	if err != nil {
		t.Fatalf("expected the 401 response returned as-is, got error %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected a single attempt for a non-retryable status, got %d", got)
	}
}

func TestDoDiscoveryRequest_PostBodyReplaysOnRetry(t *testing.T) {
	// The ollama /api/show path POSTs a JSON body; every retry must carry the
	// full body, which is why doDiscoveryRequest rebuilds the request.
	var calls atomic.Int32
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) < 2 {
			http.Error(w, "flap", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	d := retryTestService(server)
	resp, err := d.doDiscoveryRequest(context.Background(), func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), "POST", server.URL, strings.NewReader(`{"model":"m"}`))
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if len(bodies) != 2 || bodies[0] != `{"model":"m"}` || bodies[1] != `{"model":"m"}` {
		t.Fatalf("expected the body on every attempt, got %q", bodies)
	}
}

func TestFetchURL_RetriesTransient5xx(t *testing.T) {
	// End-to-end through fetchURL: one flaky 500 must not fail a listing.
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "transient", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()

	d := retryTestService(server)
	body, err := d.fetchURL(context.Background(), "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("expected fetchURL to retry the 500, got %v", err)
	}
	if !strings.Contains(string(body), "data") {
		t.Errorf("unexpected body %q", body)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestDiscoverOllama_TagsRetriesTransientFailure(t *testing.T) {
	// The whole discovery flow: a 503 on the first /api/tags fetch is retried,
	// so the scan succeeds instead of aborting on a network hiccup.
	var tagCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			if tagCalls.Add(1) == 1 {
				http.Error(w, "flap", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OllamaTagsResponse{Models: []OllamaTagsModel{{Name: "m1"}}})
		case "/api/show":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OllamaShowResponse{Capabilities: []string{"completion"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	d := retryTestService(server)
	models, err := d.discoverOllama(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "key")
	if err != nil {
		t.Fatalf("expected discovery to survive one flaky tags fetch, got %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "m1" {
		t.Fatalf("expected m1 discovered, got %v", models)
	}
	if got := tagCalls.Load(); got != 2 {
		t.Errorf("expected 2 tags attempts, got %d", got)
	}
}
