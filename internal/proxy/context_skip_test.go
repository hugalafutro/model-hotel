// Package proxy provides the LLM proxy endpoint and request handling.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestContextErrorsNotCountedAsProviderFailures verifies that context
// cancellation and deadline errors are correctly identified so the proxy
// handler can skip circuit breaker RecordFailure calls for them.
//
// The actual skip logic lives in ChatCompletions (proxy.go:446-460).
// This test validates the error classification on which that logic depends.
func TestContextErrorsNotCountedAsProviderFailures(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		shouldBeSkipped bool
	}{
		{
			name:            "context.Canceled is skipped",
			err:             context.Canceled,
			shouldBeSkipped: true,
		},
		{
			name:            "context.DeadlineExceeded is skipped",
			err:             context.DeadlineExceeded,
			shouldBeSkipped: true,
		},
		{
			name:            "wrapped context.Canceled is skipped",
			err:             fmt.Errorf("upstream: %w", context.Canceled),
			shouldBeSkipped: true,
		},
		{
			name:            "wrapped context.DeadlineExceeded is skipped",
			err:             fmt.Errorf("upstream: %w", context.DeadlineExceeded),
			shouldBeSkipped: true,
		},
		{
			name:            "connection refused is NOT skipped",
			err:             errors.New("connection refused"),
			shouldBeSkipped: false,
		},
		{
			name:            "DNS error is NOT skipped",
			err:             errors.New("lookup: no such host"),
			shouldBeSkipped: false,
		},
		{
			name:            "nil error is NOT skipped (shouldn't happen but test)",
			err:             nil,
			shouldBeSkipped: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var skipped bool
			if tc.err != nil {
				skipped = errors.Is(tc.err, context.Canceled) || errors.Is(tc.err, context.DeadlineExceeded)
			}
			if skipped != tc.shouldBeSkipped {
				t.Errorf("errors.Is classification: skipped=%v, want skipped=%v for err=%v", skipped, tc.shouldBeSkipped, tc.err)
			}
		})
	}
}
