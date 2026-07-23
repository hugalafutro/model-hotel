package ratelimit

import (
	"context"
	"testing"
)

func TestFleetDivisor(t *testing.T) {
	ctx := context.Background()

	s := newStubSettings()
	if got := fleetDivisor(ctx, s); got != 1 {
		t.Errorf("unset divisor = %d, want 1", got)
	}

	s.set(settingsKeyFleetActiveMembers, "3")
	if got := fleetDivisor(ctx, s); got != 3 {
		t.Errorf("divisor = %d, want 3", got)
	}

	// Zero and negative are floored to 1: never divide by 0, never invert the cap.
	s.set(settingsKeyFleetActiveMembers, "0")
	if got := fleetDivisor(ctx, s); got != 1 {
		t.Errorf("zero divisor = %d, want floored 1", got)
	}
	s.set(settingsKeyFleetActiveMembers, "-5")
	if got := fleetDivisor(ctx, s); got != 1 {
		t.Errorf("negative divisor = %d, want floored 1", got)
	}
}
