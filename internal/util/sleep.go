package util

import (
	"context"
	"time"
)

// SleepContext waits for d, returning early with ctx.Err() if the context is
// cancelled first. A non-positive duration returns immediately without arming
// a timer.
func SleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
