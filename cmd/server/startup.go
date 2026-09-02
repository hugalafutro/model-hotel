package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/otelexport"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// initAppLogging routes slog output through the app log pipeline so debuglog
// calls reach the ring buffer and database (not just os.Stdout). When OTLP log
// export is enabled (OTEL_EXPORTER_OTLP_ENDPOINT), fan out to it too so the
// same structured records are pushed to an OpenTelemetry collector. Returns
// the OTLP shutdown hook, nil when export is not enabled.
func initAppLogging(ctx context.Context) func(context.Context) error {
	appLogHandler := api.NewAppSlogHandler(debuglog.Level())
	var otelLogShutdown func(context.Context) error
	if otelexport.LogsEnabled() {
		otelHandler, shutdown, oerr := otelexport.NewSlogHandler(ctx, "model-hotel", debuglog.Level())
		if oerr != nil {
			debuglog.Error("otel: OTLP log export init failed; continuing without it", "error", oerr)
		} else {
			appLogHandler = debuglog.NewFanout(appLogHandler, otelHandler)
			otelLogShutdown = shutdown
		}
	}
	debuglog.SetHandler(appLogHandler)
	// Logged after SetHandler installs the fan-out, so the confirmation itself
	// is also exported to the OTLP collector.
	if otelLogShutdown != nil {
		debuglog.Info("otel: OTLP log export enabled")
	}
	return otelLogShutdown
}

// cleanupInterruptedRequests marks request logs left in "pending" or
// "streaming" state by a previous server crash, restart, or unhandled error as
// failed. serverStartTime is captured before the DB is ready, so only rows that
// predate this process are reclaimed and a long-running live streaming request
// is never touched.
func cleanupInterruptedRequests(pool *pgxpool.Pool, serverStartTime time.Time) {
	tag, err := pool.Exec(context.Background(), `
		UPDATE request_logs
		SET state = 'failed', error_kind = 'internal', error_message = 'request interrupted (server restart)'
		WHERE state IN ('pending', 'streaming')
		  AND created_at < $1`, serverStartTime)
	if err == nil && tag.RowsAffected() > 0 {
		debuglog.Info("startup: stale log cleanup", "rows", tag.RowsAffected())
		events.Publish(events.Event{
			Type:     "logs.stale_startup",
			Severity: "warning",
			Message:  fmt.Sprintf("Server restart interrupted %d pending %s", tag.RowsAffected(), util.Plural(int(tag.RowsAffected()), "request", "requests")),
			Metadata: map[string]any{"count": tag.RowsAffected()},
		})
	} else if err != nil {
		debuglog.Error("startup: stale log cleanup failed", "error", err)
	}
}

// warmCaches pre-warms caches synchronously before connections are accepted.
// Provider, model, and failover lookups are fast (simple SELECT queries), but
// key warming (Argon2id) takes ~150ms per provider, for a total under 1s for a
// handful of providers. It spares the first request the cold-cache penalty of
// ~170ms+ across the failover, model, provider, and key-decryption queries.
func warmCaches(deps discoveryDeps, settingsRepo *settings.Repository) {
	ctx := context.Background()

	providers, err := deps.providerRepo.List(ctx)
	if err != nil {
		debuglog.Error("cache: warm failed to list providers", "error", err)
	} else {
		enabledProviders := make([]*provider.Provider, 0, len(providers))
		for _, p := range providers {
			if !p.Enabled || !p.AutodiscoveryEnabled {
				continue
			}
			if len(p.EncryptedKey) > 0 {
				auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, deps.cfg.MasterKey)
			}
			enabledProviders = append(enabledProviders, p)
		}
		provider.WarmProviderCache(enabledProviders)
	}
	// Every provider key, enabled or not, joins the credential mask's held set;
	// a disabled provider is the one a relay is most likely to quote. Synchronous
	// like the warm above: the set has to be complete before the listener opens,
	// and the rows that warm just derived are cache hits here, so the extra cost
	// is the disabled and non-autodiscovery rows at a few milliseconds each.
	held, failed := provider.HoldKeys(ctx, deps.providerRepo, deps.cfg.MasterKey)
	debuglog.Info("cache: provider keys held for the credential mask", "held", held, "failed", failed)

	enabledModels, err := deps.modelRepo.ListEnabled(ctx)
	if err != nil {
		debuglog.Error("cache: warm failed to list models", "error", err)
	} else {
		model.WarmModelCache(enabledModels)
	}

	failoverGroups, err := deps.failoverRepo.List(ctx)
	if err != nil {
		debuglog.Error("cache: warm failed to list failover groups", "error", err)
	} else {
		failover.WarmFailoverCache(failoverGroups)
	}

	settingsRepo.WarmCache(ctx)

	debuglog.Info("cache: key, provider, model, failover, and settings caches warmed")
}

// initKeyCacheTTL seeds the key cache TTL from settings and reacts to changes.
func initKeyCacheTTL(settingsRepo *settings.Repository) {
	auth.SetKeyCacheTTL(settingsRepo.GetDuration(context.Background(), "key_cache_ttl", auth.DefaultKeyCacheTTL))
	settingsRepo.RegisterOnChange(func(key, value string) {
		if key == "key_cache_ttl" {
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				debuglog.Warn("keycache: invalid key_cache_ttl setting, keeping current value", "value", value, "error", err)
				return
			}
			auth.SetKeyCacheTTL(d)
			debuglog.Info("keycache: TTL updated", "ttl", d)
		}
	})
}
