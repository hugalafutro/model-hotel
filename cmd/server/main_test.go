package main

import (
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
)

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
