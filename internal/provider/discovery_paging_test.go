package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestPageCursorsAdvance(t *testing.T) {
	t.Parallel()

	c := pageCursors{}
	if err := c.advance(""); !errors.Is(err, errPageCursorMissing) {
		t.Errorf("empty cursor: got %v, want errPageCursorMissing", err)
	}
	if err := c.advance("a"); err != nil {
		t.Fatalf("first cursor rejected: %v", err)
	}
	if err := c.advance("a"); !errors.Is(err, errPageCursorRepeated) {
		t.Errorf("repeated cursor: got %v, want errPageCursorRepeated", err)
	}
	// A cursor seen two pages back is a loop as much as the previous one.
	if err := c.advance("b"); err != nil {
		t.Fatalf("second cursor rejected: %v", err)
	}
	if err := c.advance("a"); !errors.Is(err, errPageCursorRepeated) {
		t.Errorf("older repeated cursor: got %v, want errPageCursorRepeated", err)
	}
}

func TestPageCursorsAdvance_PageLimit(t *testing.T) {
	t.Parallel()

	c := pageCursors{}
	// maxDiscoveryPages pages take maxDiscoveryPages-1 cursors.
	for i := range maxDiscoveryPages - 1 {
		if err := c.advance(fmt.Sprint(i)); err != nil {
			t.Fatalf("cursor %d rejected: %v", i, err)
		}
	}
	if err := c.advance("one-too-many"); !errors.Is(err, errPageLimit) {
		t.Errorf("got %v, want errPageLimit", err)
	}
}

// anthropicPagingServer answers every /v1/models page with one model and
// has_more, taking the page's last_id from lastID (given the 1-based call
// number).
func anthropicPagingServer(t *testing.T, lastID func(call int32) string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     []map[string]any{{"id": fmt.Sprintf("model-%d", n), "type": "model"}},
			"has_more": true,
			"last_id":  lastID(n),
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestAnthropicDiscovery_PaginationFaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lastID    func(call int32) string
		wantErr   error
		wantCalls int32
	}{
		{
			name:      "repeated cursor",
			lastID:    func(int32) string { return "same" },
			wantErr:   errPageCursorRepeated,
			wantCalls: 2,
		},
		{
			name: "cursor loop over two pages",
			lastID: func(call int32) string {
				return []string{"a", "b"}[call%2]
			},
			wantErr:   errPageCursorRepeated,
			wantCalls: 3,
		},
		{
			name:      "more pages without a cursor",
			lastID:    func(int32) string { return "" },
			wantErr:   errPageCursorMissing,
			wantCalls: 1,
		},
		{
			name:      "endless fresh cursors",
			lastID:    func(call int32) string { return fmt.Sprintf("cursor-%d", call) },
			wantErr:   errPageLimit,
			wantCalls: maxDiscoveryPages,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server, calls := anthropicPagingServer(t, tt.lastID)

			svc := NewDiscoveryService(nil, nil)
			models, err := svc.discoverAnthropic(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "test-key")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got error %v, want %v", err, tt.wantErr)
			}
			// A partial catalogue would disable the models on the unread pages.
			if models != nil {
				t.Errorf("expected no models on a pagination fault, got %d", len(models))
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Errorf("expected %d requests, got %d", tt.wantCalls, got)
			}
		})
	}
}

func TestAnthropicDiscovery_EscapesCursor(t *testing.T) {
	t.Parallel()

	const cursor = "a&limit=1 b+c#d"
	var afterIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		afterIDs = append(afterIDs, r.URL.Query().Get("after_id"))
		w.Header().Set("Content-Type", "application/json")
		if len(afterIDs) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     []map[string]any{{"id": "model-1", "type": "model"}},
				"has_more": true,
				"last_id":  cursor,
			})
			return
		}
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Errorf("cursor rewrote the limit parameter to %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "model-2", "type": "model"}},
		})
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	models, err := svc.discoverAnthropic(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if len(afterIDs) != 2 || afterIDs[1] != cursor {
		t.Errorf("expected the second request to carry after_id %q, got %q", cursor, afterIDs)
	}
}

// coherePagingServer answers the chat listing with one model per page and the
// page token nextToken returns for the request's own token; the rerank listing
// is empty.
func coherePagingServer(t *testing.T, nextToken func(pageToken string) string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("endpoint") != "chat" {
			_ = json.NewEncoder(w).Encode(CohereModelsResponse{})
			return
		}
		n := calls.Add(1)
		_ = json.NewEncoder(w).Encode(CohereModelsResponse{
			Models:        []CohereNativeModel{{Name: fmt.Sprintf("command-%d", n), Endpoints: []string{"chat"}}},
			NextPageToken: nextToken(r.URL.Query().Get("page_token")),
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestDiscoverCohere_RepeatedPageToken(t *testing.T) {
	t.Parallel()

	server, calls := coherePagingServer(t, func(string) string { return "same" })

	service := &DiscoveryService{httpClient: server.Client()}
	models, err := service.discoverCohere(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "test-api-key")
	if !errors.Is(err, errPageCursorRepeated) {
		t.Fatalf("got error %v, want errPageCursorRepeated", err)
	}
	if models != nil {
		t.Errorf("expected no models on a pagination fault, got %d", len(models))
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
}

func TestDiscoverCohere_EscapesPageToken(t *testing.T) {
	t.Parallel()

	// A base64 token: unescaped, its "+" would reach the server as a space.
	const token = "ab+c/d=="
	var second string
	server, calls := coherePagingServer(t, func(pageToken string) string {
		if pageToken == "" {
			return token
		}
		second = pageToken
		return ""
	})

	service := &DiscoveryService{httpClient: server.Client()}
	models, err := service.discoverCohere(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}
	if len(models) != 2 || calls.Load() != 2 {
		t.Errorf("expected 2 models from 2 requests, got %d from %d", len(models), calls.Load())
	}
	if second != token {
		t.Errorf("expected the second request to carry page_token %q, got %q", token, second)
	}
}

func TestCursorForLog(t *testing.T) {
	t.Parallel()

	if got := cursorForLog("a\nb"); got != `"a\nb"` {
		t.Errorf("control character not quoted: %s", got)
	}
	long := strings.Repeat("x", 200)
	if got, want := cursorForLog(long), `"`+long[:64]+`"`; got != want {
		t.Errorf("got %d characters, want the first 64 quoted", len(got))
	}
}

// An empty page that still reports more pages is not the end of the listing:
// taken as one, it would return the pages read so far as the whole catalogue.
func TestAnthropicDiscovery_EmptyPageWithMore(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     []map[string]any{{"id": "model-1", "type": "model"}},
				"has_more": true,
				"last_id":  "model-1",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}, "has_more": true})
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	models, err := svc.discoverAnthropic(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "test-key")
	if !errors.Is(err, errPageCursorMissing) {
		t.Fatalf("got error %v, want errPageCursorMissing", err)
	}
	if models != nil {
		t.Errorf("expected no models, got %d", len(models))
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
}

// A rerank fetch that fails keeps the chat models, but a rerank listing whose
// paging cannot be followed fails the scan.
func TestDiscoverCohere_RerankRepeatedPageToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("endpoint") == "chat" {
			_ = json.NewEncoder(w).Encode(CohereModelsResponse{
				Models: []CohereNativeModel{{Name: "command-r", Endpoints: []string{"chat"}}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(CohereModelsResponse{
			Models:        []CohereNativeModel{{Name: "rerank-v4", Endpoints: []string{"rerank"}}},
			NextPageToken: "same",
		})
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	models, err := service.discoverCohere(context.Background(), &Provider{ID: uuid.New(), BaseURL: server.URL}, "test-api-key")
	if !errors.Is(err, errPageCursorRepeated) {
		t.Fatalf("got error %v, want errPageCursorRepeated", err)
	}
	if models != nil {
		t.Errorf("expected no models on a rerank paging fault, got %d", len(models))
	}
}
