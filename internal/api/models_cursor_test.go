package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestListModelsCursor_Default tests the default cursor request with no cursor.
func TestListModelsCursor_Default(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "cursor-test-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert 3 models with different names
	pool := h.Pool().Pool()
	for i, name := range []string{"alpha-model", "beta-model", "gamma-model"} {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), providerResp.ID, name, name, `{"vision": true}`, true)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// Request default cursor page (no cursor, sort by name asc)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Entries) < 3 {
		t.Errorf("expected at least 3 entries, got %d", len(resp.Entries))
	}
	if resp.Total < 3 {
		t.Errorf("expected total >= 3, got %d", resp.Total)
	}
	// First page: has_before should be false
	if resp.HasBefore {
		t.Error("expected HasBefore=false for first page")
	}
	// Verify sort order (name ASC default): alpha < beta < gamma
	if len(resp.Entries) >= 3 {
		if resp.Entries[0].Name > resp.Entries[1].Name || resp.Entries[1].Name > resp.Entries[2].Name {
			t.Errorf("expected entries sorted by name ASC, got: %s, %s, %s",
				resp.Entries[0].Name, resp.Entries[1].Name, resp.Entries[2].Name)
		}
	}
}

// TestListModelsCursor_WithCursor tests forward and backward cursor pagination.
func TestListModelsCursor_WithCursor(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "cursor-pager-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert 5 models with distinct names for deterministic pagination
	pool := h.Pool().Pool()
	for i, name := range []string{"a-model", "b-model", "c-model", "d-model", "e-model"} {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), providerResp.ID, name, name, `{}`, true)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// Fetch first page with limit=2
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?limit=2&sort_by=name&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("failed to decode page1: %v", err)
	}

	if len(page1.Entries) != 2 {
		t.Fatalf("expected 2 entries on page1, got %d", len(page1.Entries))
	}
	if !page1.HasAfter {
		t.Error("expected HasAfter=true when more pages exist")
	}
	if page1.HasBefore {
		t.Error("expected HasBefore=false for first page")
	}

	// Build cursor from last entry of page1
	lastEntry := page1.Entries[len(page1.Entries)-1]
	cursor := modelCursor{
		SortBy: "name",
		Name:   lastEntry.Name,
		ID:     lastEntry.ID,
	}

	// Fetch next page
	nextURL := fmt.Sprintf("/models/cursor?limit=2&sort_by=name&sort_dir=asc&cursor=%s&direction=after", url.QueryEscape(cursor.encode()))
	req = httptest.NewRequest(http.MethodGet, nextURL, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page2); err != nil {
		t.Fatalf("failed to decode page2: %v", err)
	}

	if len(page2.Entries) < 2 {
		t.Fatalf("expected at least 2 entries on page2, got %d", len(page2.Entries))
	}
	if !page2.HasBefore {
		t.Error("expected HasBefore=true when fetching after a cursor")
	}

	// Verify no overlap between pages
	page1IDs := map[string]bool{}
	for _, e := range page1.Entries {
		page1IDs[e.ID] = true
	}
	for _, e := range page2.Entries {
		if page1IDs[e.ID] {
			t.Errorf("page2 entry %s overlaps with page1", e.ID)
		}
	}
}

// TestListModelsCursor_InvalidCursor tests that an invalid cursor returns 400.
func TestListModelsCursor_InvalidCursor(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/models/cursor?cursor=not-valid-base64!!!", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid cursor, got %d", w.Code)
	}
}

// TestListModelsCursor_WithFilters tests cursor pagination with search and capabilities filters.
func TestListModelsCursor_WithFilters(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "cursor-filter-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert models with different capabilities
	pool := h.Pool().Pool()
	models := []struct {
		modelID      string
		name         string
		capabilities string
	}{
		{"vision-model", "Vision Model", `{"vision": true, "reasoning": true}`},
		{"text-model", "Text Only Model", `{"streaming": true}`},
		{"reasoning-model", "Reasoning Model", `{"reasoning": true, "tool_calling": true}`},
	}
	for i, m := range models {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), providerResp.ID, m.modelID, m.name, m.capabilities, true)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// Filter by vision capability
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?capabilities=vision", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Only the vision model should match
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry with vision capability, got %d", len(resp.Entries))
	}
	if len(resp.Entries) > 0 && resp.Entries[0].ModelID != "vision-model" {
		t.Errorf("expected vision-model, got %s", resp.Entries[0].ModelID)
	}

	// Filter by search query
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?search=reasoning+model", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var searchResp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}

	// Should match "Reasoning Model"
	if len(searchResp.Entries) != 1 {
		t.Errorf("expected 1 entry matching 'reasoning model', got %d", len(searchResp.Entries))
	}
}

// TestListModelsCursor_OutputsFilter tests filtering by output modalities.
func TestListModelsCursor_OutputsFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	providerData := fmt.Sprintf(`{"name": "cursor-outputs-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}
	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	pool := h.Pool().Pool()
	models := []struct {
		modelID string
		outputs string
	}{
		{"z-image-turbo", `["image"]`},
		{"chat-model", `["text"]`},
		{"nomic-embed", `["embedding"]`},
		{"gemini-image", `["text","image"]`},
	}
	for i, m := range models {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, output_modalities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), providerResp.ID, m.modelID, m.modelID, m.outputs, true)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	fetch := func(query string) []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/models/cursor?"+query, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", query, w.Code, w.Body.String())
		}
		var resp ModelsCursorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		ids := make([]string, 0, len(resp.Entries))
		for _, e := range resp.Entries {
			ids = append(ids, e.ModelID)
		}
		return ids
	}

	// Image output matches both the pure generator and the chat model that
	// also emits images.
	gotImage := fetch("outputs=image&provider_id=" + providerResp.ID)
	if len(gotImage) != 2 {
		t.Errorf("outputs=image: expected 2 entries, got %d (%v)", len(gotImage), gotImage)
	}

	gotEmbed := fetch("outputs=embedding&provider_id=" + providerResp.ID)
	if len(gotEmbed) != 1 || gotEmbed[0] != "nomic-embed" {
		t.Errorf("outputs=embedding: expected [nomic-embed], got %v", gotEmbed)
	}

	// Multi-value filters AND together: nothing outputs both image and
	// embedding.
	gotBoth := fetch("outputs=image,embedding&provider_id=" + providerResp.ID)
	if len(gotBoth) != 0 {
		t.Errorf("outputs=image,embedding: expected 0 entries, got %d (%v)", len(gotBoth), gotBoth)
	}

	// "text" is filterable too: the chat model and the chat model that also
	// emits images.
	gotText := fetch("outputs=text&provider_id=" + providerResp.ID)
	if len(gotText) != 2 {
		t.Errorf("outputs=text: expected 2 entries, got %d (%v)", len(gotText), gotText)
	}

	// Unknown output values are ignored rather than matching nothing.
	gotUnknown := fetch("outputs=hologram&provider_id=" + providerResp.ID)
	if len(gotUnknown) != 4 {
		t.Errorf("outputs=hologram: expected all 4 entries, got %d (%v)", len(gotUnknown), gotUnknown)
	}
}

// TestListModelsCursor_SortByDiscovered tests cursor pagination sorted by last_seen_at.
func TestListModelsCursor_SortByDiscovered(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "cursor-discovered-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert models with different last_seen_at timestamps
	pool := h.Pool().Pool()
	now := time.Now().UTC()
	models := []struct {
		modelID    string
		name       string
		lastSeenAt time.Time
	}{
		{"old-model", "Old Model", now.Add(-48 * time.Hour)},
		{"recent-model", "Recent Model", now.Add(-1 * time.Hour)},
		{"newest-model", "Newest Model", now.Add(-10 * time.Minute)},
	}
	for i, m := range models {
		id := uuid.New()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled, last_seen_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, providerResp.ID, m.modelID, m.name, `{}`, true, m.lastSeenAt)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// Sort by discovered DESC (most recent first)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?sort_by=discovered&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(resp.Entries))
	}

	// Verify order: newest > recent > old
	if resp.Entries[0].ModelID != "newest-model" {
		t.Errorf("expected first entry to be newest-model, got %s", resp.Entries[0].ModelID)
	}
	if resp.Entries[1].ModelID != "recent-model" {
		t.Errorf("expected second entry to be recent-model, got %s", resp.Entries[1].ModelID)
	}
	if resp.Entries[2].ModelID != "old-model" {
		t.Errorf("expected third entry to be old-model, got %s", resp.Entries[2].ModelID)
	}
}

// TestListModelsCursor_SortByContext tests cursor pagination sorted by context_length.
func TestListModelsCursor_SortByContext(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "cursor-ctx-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert models with different context lengths
	pool := h.Pool().Pool()
	ctxLens := []struct {
		modelID string
		name    string
		ctxLen  int
	}{
		{"small-ctx", "Small Context", 4096},
		{"medium-ctx", "Medium Context", 32768},
		{"large-ctx", "Large Context", 128000},
	}
	for i, m := range ctxLens {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled, context_length) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			uuid.New(), providerResp.ID, m.modelID, m.name, `{}`, true, m.ctxLen)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// Sort by context DESC (largest first)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?sort_by=context&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(resp.Entries))
	}

	// Verify order: large > medium > small
	if resp.Entries[0].ModelID != "large-ctx" {
		t.Errorf("expected first entry to be large-ctx, got %s", resp.Entries[0].ModelID)
	}
	if resp.Entries[1].ModelID != "medium-ctx" {
		t.Errorf("expected second entry to be medium-ctx, got %s", resp.Entries[1].ModelID)
	}
	if resp.Entries[2].ModelID != "small-ctx" {
		t.Errorf("expected third entry to be small-ctx, got %s", resp.Entries[2].ModelID)
	}
}

// TestListModelsCursor_ProviderIDFilter tests cursor pagination with provider_id filter.

// TestListModelsCursor_MultiProviderFilter tests cursor pagination with comma-separated provider_id filter.
func TestListModelsCursor_MultiProviderFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create three providers
	provider1Data := fmt.Sprintf(`{"name": "multi-prov1-%s", "base_url": "https://api.example.com", "api_key": "test-key1"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider1: %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider1 response: %v", err)
	}

	provider2Data := fmt.Sprintf(`{"name": "multi-prov2-%s", "base_url": "https://api.anthropic.com", "api_key": "test-key2"}`, uuid.New().String()[:8])
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider2: %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider2 response: %v", err)
	}

	provider3Data := fmt.Sprintf(`{"name": "multi-prov3-%s", "base_url": "https://api.example.com/3", "api_key": "test-key3"}`, uuid.New().String()[:8])
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider3Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider3: %d: %s", rec.Code, rec.Body.String())
	}

	var provider3Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider3Resp); err != nil {
		t.Fatalf("Failed to parse provider3 response: %v", err)
	}

	// Insert models for all three providers
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), provider1Resp.ID, "p1-model", "P1 Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert p1 model: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), provider2Resp.ID, "p2-model", "P2 Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert p2 model: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), provider3Resp.ID, "p3-model", "P3 Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert p3 model: %v", err)
	}

	// Filter by both provider1 AND provider2 (comma-separated)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?provider_id="+provider1Resp.ID+","+provider2Resp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries for provider1+provider2, got %d", len(resp.Entries))
	}
	// Verify provider3's model is excluded
	for _, entry := range resp.Entries {
		if entry.ModelID == "p3-model" {
			t.Error("p3-model should not appear in filtered results")
		}
	}
}

// TestListModelsCursor_SortByProvider tests cursor pagination sorted by provider name.
func TestListModelsCursor_SortByProvider(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create two providers with names that sort deterministically
	providerAlphaData := fmt.Sprintf(`{"name": "alpha-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key-alpha"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerAlphaData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create alpha provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerAlphaResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerAlphaResp); err != nil {
		t.Fatalf("Failed to parse alpha provider response: %v", err)
	}

	providerZetaData := fmt.Sprintf(`{"name": "zeta-provider-%s", "base_url": "https://api.anthropic.com", "api_key": "test-key-zeta"}`, uuid.New().String()[:8])
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerZetaData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create zeta provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerZetaResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerZetaResp); err != nil {
		t.Fatalf("Failed to parse zeta provider response: %v", err)
	}

	// Insert 2 models: one for each provider
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), providerAlphaResp.ID, "a-model", "Alpha Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert alpha model: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), providerZetaResp.ID, "z-model", "Zeta Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert zeta model: %v", err)
	}

	// Request sort by provider ASC
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?sort_by=provider&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var respAsc ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &respAsc); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(respAsc.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(respAsc.Entries))
	}

	// Verify order: alpha-provider's model comes first
	if respAsc.Entries[0].ModelID != "a-model" {
		t.Errorf("expected first entry to be a-model (alpha provider), got %s", respAsc.Entries[0].ModelID)
	}
	if respAsc.Entries[1].ModelID != "z-model" {
		t.Errorf("expected second entry to be z-model (zeta provider), got %s", respAsc.Entries[1].ModelID)
	}

	// Request sort by provider DESC
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?sort_by=provider&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var respDesc ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &respDesc); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(respDesc.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(respDesc.Entries))
	}

	// Verify order: zeta-provider's model comes first
	if respDesc.Entries[0].ModelID != "z-model" {
		t.Errorf("expected first entry to be z-model (zeta provider), got %s", respDesc.Entries[0].ModelID)
	}
	if respDesc.Entries[1].ModelID != "a-model" {
		t.Errorf("expected second entry to be a-model (alpha provider), got %s", respDesc.Entries[1].ModelID)
	}
}

// TestListModelsCursor_SortByStatus tests cursor pagination sorted by status.
func TestListModelsCursor_SortByStatus(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create one provider
	providerData := fmt.Sprintf(`{"name": "cursor-status-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert 3 models with different status states
	pool := h.Pool().Pool()
	// Active model: enabled=true, disabled_manually=false (status_sort=0)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled, disabled_manually) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), providerResp.ID, "active-model", "Active Model", `{}`, true, false)
	if err != nil {
		t.Fatalf("Failed to insert active model: %v", err)
	}
	// Manual-disabled model: enabled=true, disabled_manually=true (status_sort=1)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled, disabled_manually) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), providerResp.ID, "manual-disabled-model", "Manual Disabled Model", `{}`, true, true)
	if err != nil {
		t.Fatalf("Failed to insert manual-disabled model: %v", err)
	}
	// Disabled model: enabled=false, disabled_manually=false (status_sort=2)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled, disabled_manually) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), providerResp.ID, "disabled-model", "Disabled Model", `{}`, false, false)
	if err != nil {
		t.Fatalf("Failed to insert disabled model: %v", err)
	}

	// Request sort by status ASC: active (0) → manual-disabled (1) → disabled (2)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?sort_by=status&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var respAsc ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &respAsc); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(respAsc.Entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(respAsc.Entries))
	}

	// Verify order: active → manual-disabled → disabled
	if respAsc.Entries[0].ModelID != "active-model" {
		t.Errorf("expected first entry to be active-model, got %s", respAsc.Entries[0].ModelID)
	}
	if respAsc.Entries[1].ModelID != "manual-disabled-model" {
		t.Errorf("expected second entry to be manual-disabled-model, got %s", respAsc.Entries[1].ModelID)
	}
	if respAsc.Entries[2].ModelID != "disabled-model" {
		t.Errorf("expected third entry to be disabled-model, got %s", respAsc.Entries[2].ModelID)
	}

	// Request sort by status DESC: disabled (2) → manual-disabled (1) → active (0)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?sort_by=status&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var respDesc ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &respDesc); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(respDesc.Entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(respDesc.Entries))
	}

	// Verify order: disabled → manual-disabled → active
	if respDesc.Entries[0].ModelID != "disabled-model" {
		t.Errorf("expected first entry to be disabled-model, got %s", respDesc.Entries[0].ModelID)
	}
	if respDesc.Entries[1].ModelID != "manual-disabled-model" {
		t.Errorf("expected second entry to be manual-disabled-model, got %s", respDesc.Entries[1].ModelID)
	}
	if respDesc.Entries[2].ModelID != "active-model" {
		t.Errorf("expected third entry to be active-model, got %s", respDesc.Entries[2].ModelID)
	}
}

func TestListModelsCursor_ProviderIDFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create two providers
	provider1Data := fmt.Sprintf(`{"name": "cursor-prov1-%s", "base_url": "https://api.example.com", "api_key": "test-key1"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider1: %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider1 response: %v", err)
	}

	provider2Data := fmt.Sprintf(`{"name": "cursor-prov2-%s", "base_url": "https://api.anthropic.com", "api_key": "test-key2"}`, uuid.New().String()[:8])
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider2: %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider2 response: %v", err)
	}

	// Insert models for both providers
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), provider1Resp.ID, "p1-model", "P1 Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert p1 model: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), provider2Resp.ID, "p2-model", "P2 Model", `{}`, true)
	if err != nil {
		t.Fatalf("Failed to insert p2 model: %v", err)
	}

	// Filter by provider1
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?provider_id="+provider1Resp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry for provider1, got %d", len(resp.Entries))
	}
	if len(resp.Entries) > 0 && resp.Entries[0].ModelID != "p1-model" {
		t.Errorf("expected p1-model, got %s", resp.Entries[0].ModelID)
	}
}

// TestListModelsCursor_BackwardPagination tests that direction=before returns
// the items immediately preceding the cursor (not items from the start of the
// dataset) and that results are in the requested sort order.
func TestListModelsCursor_BackwardPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "backward-page-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert 10 models with distinct names
	pool := h.Pool().Pool()
	names := []string{"m01", "m02", "m03", "m04", "m05", "m06", "m07", "m08", "m09", "m10"}
	for _, name := range names {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), providerResp.ID, name, name, `{}`, true)
		if err != nil {
			t.Fatalf("Failed to insert model %s: %v", name, err)
		}
	}

	// Fetch forward with limit=3 to get page at m07-m09
	// Page 1: m01, m02, m03 (cursor at m03)
	req = httptest.NewRequest(http.MethodGet, "/models/cursor?limit=3&sort_by=name&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("failed to decode page1: %v", err)
	}

	// Use last entry of page1 as cursor to get page 2 (m04-m06)
	page1Last := page1.Entries[len(page1.Entries)-1]
	page1Cursor := modelCursor{SortBy: "name", Name: page1Last.Name, ID: page1Last.ID}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/models/cursor?limit=3&sort_by=name&sort_dir=asc&cursor=%s&direction=after", url.QueryEscape(page1Cursor.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page2); err != nil {
		t.Fatalf("failed to decode page2: %v", err)
	}

	if len(page2.Entries) != 3 {
		t.Fatalf("expected 3 entries on page2, got %d", len(page2.Entries))
	}

	// Use last entry of page2 as cursor to get page 3 (m07-m09)
	page2Last := page2.Entries[len(page2.Entries)-1]
	page2Cursor := modelCursor{SortBy: "name", Name: page2Last.Name, ID: page2Last.ID}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/models/cursor?limit=3&sort_by=name&sort_dir=asc&cursor=%s&direction=after", url.QueryEscape(page2Cursor.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page3 ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page3); err != nil {
		t.Fatalf("failed to decode page3: %v", err)
	}

	if len(page3.Entries) != 3 {
		t.Fatalf("expected 3 entries on page3, got %d", len(page3.Entries))
	}

	// Now use page3's first entry as cursor with direction=before, limit=3
	// This should return the 3 items immediately before page3: m04, m05, m06
	backwardCursor := modelCursor{
		SortBy: "name",
		Name:   page3.Entries[0].Name,
		ID:     page3.Entries[0].ID,
	}

	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/models/cursor?limit=3&sort_by=name&sort_dir=asc&cursor=%s&direction=before", url.QueryEscape(backwardCursor.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var beforePage ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &beforePage); err != nil {
		t.Fatalf("failed to decode before page: %v", err)
	}

	if len(beforePage.Entries) != 3 {
		t.Fatalf("expected 3 entries for backward page, got %d", len(beforePage.Entries))
	}

	// Results must be in ASC order (the requested sort_dir)
	if beforePage.Entries[0].Name != "m04" {
		t.Errorf("expected first entry 'm04', got '%s'", beforePage.Entries[0].Name)
	}
	if beforePage.Entries[1].Name != "m05" {
		t.Errorf("expected second entry 'm05', got '%s'", beforePage.Entries[1].Name)
	}
	if beforePage.Entries[2].Name != "m06" {
		t.Errorf("expected third entry 'm06', got '%s'", beforePage.Entries[2].Name)
	}

	// Must have has_after=true (items exist after the cursor by definition)
	if !beforePage.HasAfter {
		t.Error("expected HasAfter=true for backward page with cursor")
	}

	// Must have has_before=true since m01-m03 still precede this page
	if !beforePage.HasBefore {
		t.Error("expected HasBefore=true for backward page (more items precede)")
	}
}

// ---------------------------------------------------------------------------
// buildModelKeysetPredicate unit tests
// ---------------------------------------------------------------------------

func TestBuildModelKeysetPredicate_EmptyCursor(t *testing.T) {
	argIdx := 1
	var args []any
	result := buildModelKeysetPredicate(modelCursor{}, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string for empty cursor, got %q", result)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %d", len(args))
	}
}

func TestBuildModelKeysetPredicate_EmptyID(t *testing.T) {
	argIdx := 1
	var args []any
	result := buildModelKeysetPredicate(modelCursor{SortBy: "name"}, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string for empty ID, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_NameSort(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "name", Name: "my-model", ID: "test-id-1"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate")
	}
	if !strings.Contains(result, ">") {
		t.Errorf("expected '>' operator for after+ASC, got %q", result)
	}
	if !strings.Contains(result, "$1") || !strings.Contains(result, "$2") {
		t.Errorf("expected $1 and $2 placeholders, got %q", result)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[0] != "my-model" {
		t.Errorf("expected first arg 'my-model', got %v", args[0])
	}
	if args[1] != "test-id-1" {
		t.Errorf("expected second arg 'test-id-1', got %v", args[1])
	}
}

func TestBuildModelKeysetPredicate_DescAfterUsesLessThan(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "name", Name: "my-model", ID: "test-id-2"}
	result := buildModelKeysetPredicate(cursor, "after", "DESC", &argIdx, &args)
	if !strings.Contains(result, "<") {
		t.Errorf("expected '<' operator for after+DESC, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_BeforeAscUsesLessThan(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "name", Name: "my-model", ID: "test-id-3"}
	result := buildModelKeysetPredicate(cursor, "before", "ASC", &argIdx, &args)
	if !strings.Contains(result, "<") {
		t.Errorf("expected '<' operator for before+ASC, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_DiscoveredSort(t *testing.T) {
	argIdx := 5
	var args []any
	now := time.Now()
	cursor := modelCursor{SortBy: "discovered", LastSeenAt: now, ID: "test-id-disc"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate for discovered sort")
	}
	if !strings.Contains(result, "last_seen_at") {
		t.Errorf("expected last_seen_at reference, got %q", result)
	}
	if argIdx != 7 {
		t.Errorf("expected argIdx=7 after appending 2 args, got %d", argIdx)
	}
}

func TestBuildModelKeysetPredicate_DiscoveredSortZeroTime(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "discovered", ID: "test-id-zero"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string when LastSeenAt is zero, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_ContextSort(t *testing.T) {
	argIdx := 3
	var args []any
	ctxLen := 8192
	cursor := modelCursor{SortBy: "context", ContextLength: &ctxLen, ID: "test-id-ctx"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate for context sort")
	}
	if !strings.Contains(result, "context_length") {
		t.Errorf("expected context_length reference, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_ContextSortNilLength(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "context", ID: "test-id-ctx-nil"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string when ContextLength is nil, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_OutputSort(t *testing.T) {
	argIdx := 1
	var args []any
	maxOut := 4096
	cursor := modelCursor{SortBy: "output", MaxOutput: &maxOut, ID: "test-id-out"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate for output sort")
	}
	if !strings.Contains(result, "max_output_tokens") {
		t.Errorf("expected max_output_tokens reference, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_OutputSortNilOutput(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "output", ID: "test-id-out-nil"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string when MaxOutput is nil, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_ProviderSort(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "provider", ProviderName: "OpenAI", ID: "test-id-prov"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate for provider sort")
	}
	if !strings.Contains(result, "p.name") {
		t.Errorf("expected p.name reference, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_ProviderSortEmptyName(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "provider", ID: "test-id-prov-empty"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string when ProviderName is empty, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_StatusSort(t *testing.T) {
	argIdx := 1
	var args []any
	statusSort := 0
	cursor := modelCursor{SortBy: "status", StatusSort: &statusSort, ID: "test-id-status"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate for status sort")
	}
	if !strings.Contains(result, "CASE") {
		t.Errorf("expected CASE expression, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_StatusSortNil(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "status", ID: "test-id-status-nil"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result != "" {
		t.Errorf("expected empty string when StatusSort is nil, got %q", result)
	}
}

func TestBuildModelKeysetPredicate_DefaultSortFallsBackToModelID(t *testing.T) {
	argIdx := 1
	var args []any
	cursor := modelCursor{SortBy: "name", ModelID: "gpt-4", ID: "test-id-fallback"}
	result := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if result == "" {
		t.Fatal("expected non-empty predicate for default sort")
	}
	// When Name is empty, it falls back to ModelID
	if len(args) < 1 {
		t.Fatalf("expected at least 1 arg, got %d", len(args))
	}
	if args[0] != "gpt-4" {
		t.Errorf("expected first arg 'gpt-4' (ModelID fallback), got %v", args[0])
	}
}

func TestBuildModelKeysetPredicate_ArgIdxAdvances(t *testing.T) {
	argIdx := 10
	var args []any
	ctxLen := 128
	cursor := modelCursor{SortBy: "context", ContextLength: &ctxLen, ID: "test-id-idx"}
	buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if argIdx != 12 {
		t.Errorf("expected argIdx=12 after starting at 10, got %d", argIdx)
	}
}

// ---------------------------------------------------------------------------
// joinAnd unit tests
// ---------------------------------------------------------------------------

func TestJoinAnd_EmptySlice(t *testing.T) {
	result := joinAnd([]string{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestJoinAnd_SingleCondition(t *testing.T) {
	result := joinAnd([]string{"a = 1"})
	if result != "a = 1" {
		t.Errorf("expected 'a = 1', got %q", result)
	}
}

func TestJoinAnd_MultipleConditions(t *testing.T) {
	result := joinAnd([]string{"a = 1", "b = 2", "c = 3"})
	if result != "a = 1 AND b = 2 AND c = 3" {
		t.Errorf("expected 'a = 1 AND b = 2 AND c = 3', got %q", result)
	}
}

func TestJoinAnd_TwoConditions(t *testing.T) {
	result := joinAnd([]string{"x > 0", "y < 10"})
	if result != "x > 0 AND y < 10" {
		t.Errorf("expected 'x > 0 AND y < 10', got %q", result)
	}
}

// ---------------------------------------------------------------------------
// modelSortColumn unit tests
// ---------------------------------------------------------------------------

// TestListModelsCursor_NilPool tests that ListModelsCursor returns an empty
// cursor response when the handler has no database pool (nil dbPool early return).
func TestListModelsCursor_NilPool(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/models/cursor", http.NoBody)
	w := httptest.NewRecorder()
	h.ListModelsCursor(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp ModelsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(resp.Entries))
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

// TestListModelsCursor_CancelledContext tests that ListModelsCursor returns
// a 500 error when the request context is already cancelled.
func TestListModelsCursor_CancelledContext(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/models/cursor", http.NoBody).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestModelSortColumn_Defaults(t *testing.T) {
	tests := []struct {
		sortBy   string
		expected string
	}{
		{"name", "COALESCE(m.name, m.model_id, '')"},
		{"discovered", "COALESCE(m.last_seen_at, m.created_at)"},
		{"context", "COALESCE(m.context_length, 0)"},
		{"output", "COALESCE(m.max_output_tokens, 0)"},
		{"provider", "COALESCE(p.name, '')"},
		{"", "COALESCE(m.name, m.model_id, '')"},
		{"unknown", "COALESCE(m.name, m.model_id, '')"},
	}
	for _, tc := range tests {
		t.Run(tc.sortBy, func(t *testing.T) {
			result := modelSortColumn(tc.sortBy)
			if result != tc.expected {
				t.Errorf("modelSortColumn(%q) = %q, want %q", tc.sortBy, result, tc.expected)
			}
		})
	}
}

// TestListModelsCursor_DirectionBefore covers the fetch-direction flip in
// ListModelsCursor: with direction=before and a DESC sort, the fetch order is
// inverted to ASC (the else branch).
func TestListModelsCursor_DirectionBefore(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/models/cursor?direction=before&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
