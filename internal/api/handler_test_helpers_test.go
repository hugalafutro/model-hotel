package api

import (
	"context"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

const testMasterKey = "testmasterkey1234567890abcdef"

// TestMain is defined in failover_api_test.go

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}

	// Clean test data within our isolated database (safe since each package has its own DB)
	pool.Exec(context.Background(), `
		TRUNCATE providers, models, virtual_keys, request_logs,
		       app_logs, model_failover_groups, settings CASCADE
	`)

	// Flush logs cache so prior test results don't leak.
	globalLogsCache.clear()

	// Create database instance
	database, err := db.New(context.Background(), dbURL, 5, 1)
	if err != nil {
		t.Skip("skipping: test database not available")
	}

	cfg := &config.Config{
		MasterKey:            testMasterKey,
		AllowHTTPProviders:   true,
		RateLimitEnabled:     false,
		DataDir:              t.TempDir(),
		AllowedProviderHosts: []string{"localhost", "127.0.0.1", "api.nano-gpt.com", "nano-gpt.com", "api.nanogpt.com", "nanogpt.com", "ngc.nanogpt.com", "openrouter.ai", "api.z.ai", "z.ai", "api.zai.chat", "zai.api.example.com", "api.deepseek.com", "deepseek.com", "api.anthropic.com", "anthropic.com", "api.openai.com", "opencode.ai", "api.example.com", "custom.example.com", "api.alpha.com", "api.beta.com", "api.first.com", "api.second.com", "api.generic.com", "example.com", "api.mistral.ai", "api.cohere.ai", "api.x.ai", "generativelanguage.googleapis.com", "192.168.1.1", "192.0.2.1", "httpbin.org"},
	}

	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)

	// Create a temporary directory for admin token
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)

	handler := NewHandler(cfg, providerRepo, database, adminMgr, vkRepo, settingsRepo, "test", nil)
	if handler == nil {
		pool.Close()
		t.Fatal("handler is nil")
	}

	t.Cleanup(func() {
		database.Close()
		pool.Close()
	})

	return handler
}

func newTestHandlerWithRouter(t *testing.T) (*Handler, chi.Router) {
	t.Helper()
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)
	// Mock Docker stats collector to avoid real Docker API calls which
	// spawn persistent HTTP transport goroutines that hang the test process.
	h.SetDockerStatsCollector(func(filter util.ContainerFilter) util.AggregatedDockerStats {
		return util.AggregatedDockerStats{}
	})
	return h, r
}
