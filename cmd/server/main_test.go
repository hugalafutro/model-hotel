package main

import (
	"context"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// TestInitAppLogging covers the non-OTLP path: the app slog handler is
// installed and no shutdown hook is returned. The stdout handler is restored
// afterwards so later tests keep the default logging destination.
func TestInitAppLogging(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	api.InitAppLogBuffer(cmdTestDB.Pool())
	defer debuglog.SetHandler(debuglog.StdoutHandler())

	if shutdown := initAppLogging(context.Background()); shutdown != nil {
		t.Error("expected no OTLP shutdown hook without OTEL_EXPORTER_OTLP_ENDPOINT")
	}
}

// TestPublishDiscoveryEvent verifies that publishDiscoveryEvent selects the
// correct severity for each outcome (all-failed, partial-failure, success) and
// publishes a discovery.complete event to the bus.
func TestPublishDiscoveryEvent(t *testing.T) {
	cases := []struct {
		name     string
		result   DiscoveryResult
		severity string
		// wantPruned is the models_pruned metadata the event must carry;
		// -1 means the key must be absent, which is the all-failed case: no
		// provider was scanned, so no prune could have run.
		wantPruned int
	}{
		{
			name:       "all_failed",
			result:     DiscoveryResult{ProvidersScanned: 0, Errors: []string{"boom"}},
			severity:   "error",
			wantPruned: -1,
		},
		{
			name:       "partial_failure",
			result:     DiscoveryResult{ProvidersScanned: 3, ProvidersFailed: 1, ModelsDiscovered: 5, ModelsPruned: 2, Errors: []string{"one failed"}},
			severity:   "warning",
			wantPruned: 2,
		},
		{
			// No provider failed, but the failover sync could not read one
			// provider's model list: still a warning, never a clean success.
			name:       "sync_input_error_without_failed_provider",
			result:     DiscoveryResult{ProvidersScanned: 3, ModelsDiscovered: 9, ModelsPruned: 1, Errors: []string{"provider x: list models for failover sync: boom"}},
			severity:   "warning",
			wantPruned: 1,
		},
		{
			name:       "success",
			result:     DiscoveryResult{ProvidersScanned: 3, ModelsDiscovered: 9, ModelsPruned: 4},
			severity:   "success",
			wantPruned: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := events.DefaultBus.Subscribe()
			defer events.DefaultBus.Unsubscribe(ch)

			publishDiscoveryEvent("startup", tc.result)

			deadline := time.After(2 * time.Second)
			for {
				select {
				case ev := <-ch:
					// Ignore unrelated events from other concurrent tests.
					if ev.Type != "discovery.complete" {
						continue
					}
					if ev.Severity != tc.severity {
						t.Errorf("expected severity %q, got %q", tc.severity, ev.Severity)
					}
					got, ok := ev.Metadata["models_pruned"]
					switch {
					case tc.wantPruned < 0:
						if ok {
							t.Errorf("models_pruned = %v, want absent when nothing was scanned", got)
						}
					case !ok:
						t.Errorf("models_pruned missing, want %d", tc.wantPruned)
					case got != tc.wantPruned:
						t.Errorf("models_pruned = %v, want %d", got, tc.wantPruned)
					}
					return
				case <-deadline:
					t.Fatal("timed out waiting for discovery.complete event")
				}
			}
		})
	}
}
