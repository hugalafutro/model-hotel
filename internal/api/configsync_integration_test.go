package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// TestConfigSync_ImportThroughRealHandlerRunsDiscovery drives /config/import via
// the full admin Handler (not the unit router), so the discovery callback wired
// in Register, func(ctx) { h.discoverAllProviders(ctx) }, actually runs as part
// of apply(). With no providers the discovery pass is a fast no-op, but it
// exercises the real wiring end to end (the closure plus the discoverAllProviders
// prologue) that the stubbed unit tests cannot reach.
func TestConfigSync_ImportThroughRealHandlerRunsDiscovery(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("test database not available")
	}
	cleanConfigTables(t)

	pool := apiTestDB.Pool()
	// A virtual key keeps the envelope non-empty (the import refuses a structurally
	// empty config) while leaving the provider set empty, so the discovery pass
	// invoked by the real callback stays a fast no-op with no upstream HTTP.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO virtual_keys (name, key_hash, key_preview) VALUES ('sync-it', 'hash-sync-it', 'sk-***')`); err != nil {
		t.Fatalf("seed virtual key: %v", err)
	}
	adminMgr, _, err := admin.New(t.TempDir(), "test-admin-token")
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	cfg := &config.Config{
		MasterKey:          configSyncMasterKey,
		AllowHTTPProviders: true,
		DataDir:            t.TempDir(),
	}
	h := NewHandler(cfg, provider.NewRepository(pool), apiTestDB, adminMgr,
		virtualkey.NewRepository(pool), settings.NewRepository(pool), "v-test", nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	authed := func(method, path string, body []byte) *httptest.ResponseRecorder {
		var rdr *bytes.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		var req *http.Request
		if rdr != nil {
			req = httptest.NewRequest(method, path, rdr)
		} else {
			req = httptest.NewRequest(method, path, http.NoBody)
		}
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// Export this (empty) member, then import it back through the same real router
	// so apply() runs and invokes the discovery callback.
	exportRec := authed(http.MethodGet, "/config/export", nil)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body %s", exportRec.Code, exportRec.Body.String())
	}

	importRec := authed(http.MethodPost, "/config/import", exportRec.Body.Bytes())
	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", importRec.Code, importRec.Body.String())
	}
	var resp importResponse
	if err := json.Unmarshal(importRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if !resp.Applied {
		t.Fatalf("expected applied=true, got %+v", resp)
	}
}
