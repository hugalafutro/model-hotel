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
		t.Skip("test DB unavailable")
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
	}{
		{
			name:     "all_failed",
			result:   DiscoveryResult{ProvidersScanned: 0, Errors: []string{"boom"}},
			severity: "error",
		},
		{
			name:     "partial_failure",
			result:   DiscoveryResult{ProvidersScanned: 3, ProvidersFailed: 1, ModelsDiscovered: 5, Errors: []string{"one failed"}},
			severity: "warning",
		},
		{
			name:     "success",
			result:   DiscoveryResult{ProvidersScanned: 3, ModelsDiscovered: 9},
			severity: "success",
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
					return
				case <-deadline:
					t.Fatal("timed out waiting for discovery.complete event")
				}
			}
		})
	}
}
