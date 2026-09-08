package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// ---------------------------------------------------------------------------
// discoverOpenCodeGo — additional paths not in discovery_http_test.go
// ---------------------------------------------------------------------------

func TestDiscoverOpenCodeGo_404FallsBackToCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-opencode-go",
		BaseURL: server.URL + "/v1",
	}

	models, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}

	// A 404 listing falls back to the catalog without erroring. The catalog is
	// an override channel that is normally empty, so this is exactly its
	// current (possibly zero) row count — never an aborted scan.
	if len(models) != len(GetOpenCodeGoCatalog()) {
		t.Errorf("Expected the catalog rows from fallback after 404, got %d models", len(models))
	}

	for _, m := range models {
		if m.ProviderID != provider.ID {
			t.Errorf("ProviderID = %v, want %v", m.ProviderID, provider.ID)
		}
		if !m.Enabled {
			t.Error("Expected model to be enabled")
		}
	}
}

func TestDiscoverOpenCodeGo_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	_, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestDiscoverOpenCodeGo_UnknownModel_MinimalEntry(t *testing.T) {
	// Test that a model not in the catalog gets a minimal entry
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{ID: "future-unknown-model-xyz", Object: "model", OwnedBy: "opencode"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	models, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}
	// The unknown live model is unioned with the catalog.
	if len(models) != len(GetOpenCodeGoCatalog())+1 {
		t.Fatalf("Expected catalog+1 merged models, got %d", len(models))
	}

	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "future-unknown-model-xyz" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected unknown live model present in merged results")
	}
	if m.OwnedBy != "opencode" {
		t.Errorf("Expected OwnedBy 'opencode', got %q", m.OwnedBy)
	}

	// Unknown model should have streaming capability only
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("Expected Streaming capability to be true")
	}
}

// Catalog-entry backfill onto a live model is covered at unit level by
// TestMergeLiveAndCatalog_LiveWinsCatalogBackfills in catalog_merge_test.go;
// the Go catalog is an override channel that is normally empty, so there is
// no shipped row to assert against through the discoverer here.

func TestDiscoverOpenCodeGo_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	_, err := service.discoverOpenCodeGo(context.Background(), provider, "wrong-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetOpenCodeGoUsage
// ---------------------------------------------------------------------------

// openCodeGoUsageBody is the live /usage payload of an active Go subscription.
const openCodeGoUsageBody = `{"usage":{` +
	`"rolling":{"status":"ok","percent":12,"resetsAt":"2026-09-08T18:25:46.155Z"},` +
	`"weekly":{"status":"ok","percent":0,"resetsAt":"2026-09-14T00:00:00.155Z"},` +
	`"monthly":{"status":"ok","percent":3.5,"resetsAt":"2026-10-08T13:25:13.155Z"}}}`

func TestGetOpenCodeGoUsage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openCodeGoUsageBody))
	}))
	defer server.Close()

	masterKey := "test-master-key-for-testing-only-32bytes!"
	prov, err := newQuotaTestProvider(server.URL, "test-api-key", masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := &DiscoveryService{httpClient: server.Client()}

	usage, err := service.GetOpenCodeGoUsage(context.Background(), prov, masterKey)
	if err != nil {
		t.Fatalf("GetOpenCodeGoUsage failed: %v", err)
	}
	if usage == nil {
		t.Fatal("expected a usage payload for an active subscription")
	}
	if usage.Usage.Rolling.Percent != 12 || usage.Usage.Rolling.Status != "ok" {
		t.Errorf("rolling = %+v, want percent 12 status ok", usage.Usage.Rolling)
	}
	if usage.Usage.Rolling.ResetsAt != "2026-09-08T18:25:46.155Z" {
		t.Errorf("rolling resetsAt = %q, want the fixture value", usage.Usage.Rolling.ResetsAt)
	}
	if usage.Usage.Weekly.Percent != 0 {
		t.Errorf("weekly percent = %v, want 0", usage.Usage.Weekly.Percent)
	}
	// Fractional percent must survive: the field is a float64 precisely so a
	// 3.5% month is not rounded away before the dashboard sees it.
	if usage.Usage.Monthly.Percent != 3.5 {
		t.Errorf("monthly percent = %v, want 3.5", usage.Usage.Monthly.Percent)
	}
}

// TestGetOpenCodeGoUsage_KeyInvalid401 covers a revoked Zen key: /usage answers
// 401 AuthError, which must stay an ErrProviderKeyInvalid (424 + WARN) rather
// than being absorbed by the no-subscription branch.
func TestGetOpenCodeGoUsage_KeyInvalid401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`))
	}))
	defer server.Close()

	masterKey := "test-master-key-for-testing-only-32bytes!"
	prov, err := newQuotaTestProvider(server.URL, "revoked-key", masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := &DiscoveryService{httpClient: server.Client()}

	usage, err := service.GetOpenCodeGoUsage(context.Background(), prov, masterKey)
	if !errors.Is(err, ErrProviderKeyInvalid) {
		t.Fatalf("expected ErrProviderKeyInvalid, got %v", err)
	}
	if usage != nil {
		t.Error("expected no payload for a rejected key")
	}
}

// TestGetOpenCodeGoUsage_NoSubscription403 covers a healthy key without an
// active Go subscription: 403 EntitlementError is the expected answer for that
// plan, so it reports no data and no error (204 + null payload upstream) and
// must not reach either the ERROR log or the dead-key path.
func TestGetOpenCodeGoUsage_NoSubscription403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"EntitlementError","message":"OpenCode Go subscription required."}}`))
	}))
	defer server.Close()

	var logged strings.Builder
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { debuglog.SetHandler(debuglog.StdoutHandler()) })

	masterKey := "test-master-key-for-testing-only-32bytes!"
	prov, err := newQuotaTestProvider(server.URL, "zen-key-without-go", masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := &DiscoveryService{httpClient: server.Client()}

	// The nil error is what proves the 403 EntitlementError was not classified
	// as a dead key: ErrProviderKeyInvalid would surface here.
	usage, err := service.GetOpenCodeGoUsage(context.Background(), prov, masterKey)
	if err != nil || usage != nil {
		t.Fatalf("GetOpenCodeGoUsage = (%v, %v), want (nil, nil)", usage, err)
	}
	if strings.Contains(logged.String(), "level=ERROR") || strings.Contains(logged.String(), "level=WARN") {
		t.Errorf("no-subscription 403 logged above INFO: %s", logged.String())
	}
	if !strings.Contains(logged.String(), "no active Go subscription") {
		t.Errorf("expected the no-subscription INFO line, got: %s", logged.String())
	}
}

func TestGetOpenCodeGoUsage_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"rolling":`))
	}))
	defer server.Close()

	masterKey := "test-master-key-for-testing-only-32bytes!"
	prov, err := newQuotaTestProvider(server.URL, "test-api-key", masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := &DiscoveryService{httpClient: server.Client()}

	usage, err := service.GetOpenCodeGoUsage(context.Background(), prov, masterKey)
	if err == nil {
		t.Fatal("expected an error for a truncated body")
	}
	if usage != nil {
		t.Error("expected no payload for a truncated body")
	}
}
