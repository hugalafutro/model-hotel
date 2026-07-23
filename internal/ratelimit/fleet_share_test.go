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

func TestFleetShareTPM(t *testing.T) {
	ctx := context.Background()

	s := newStubSettings()
	s.set(settingsKeyFleetActiveMembers, "4")
	if got := fleetShareTPM(ctx, s, 800); got != 200 {
		t.Errorf("shared tpm = %d, want 200", got)
	}
	// Unlimited (tpm<=0) is left untouched — never turned into a finite cap.
	if got := fleetShareTPM(ctx, s, 0); got != 0 {
		t.Errorf("unlimited tpm = %d, want 0", got)
	}
	// Floor: 2/4 == 0 must floor to 1 (a 0 TPM reads as "no cap").
	if got := fleetShareTPM(ctx, s, 2); got != 1 {
		t.Errorf("floored tpm = %d, want 1", got)
	}

	// Divisor 1 (standalone) is a no-op.
	s1 := newStubSettings()
	if got := fleetShareTPM(ctx, s1, 600); got != 600 {
		t.Errorf("standalone tpm = %d, want 600", got)
	}
}
