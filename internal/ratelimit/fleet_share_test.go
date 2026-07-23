package ratelimit

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// setFleetActive sets the divisor count together with a fresh refresh timestamp,
// the state a live divisor-aware announce leaves behind. Tests that expect the
// divisor to be honored must use this (a count without a fresh timestamp is
// treated as stale and reverts to 1).
func setFleetActive(s *stubSettings, n int) {
	s.set(settingsKeyFleetActiveMembers, strconv.Itoa(n))
	s.set(settingsKeyFleetActiveMembersAt, strconv.FormatInt(time.Now().Unix(), 10))
}

func TestFleetDivisor(t *testing.T) {
	ctx := context.Background()

	s := newStubSettings()
	if got := fleetDivisor(ctx, s); got != 1 {
		t.Errorf("unset divisor = %d, want 1", got)
	}

	setFleetActive(s, 3)
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
	setFleetActive(s, 4)
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

func TestFleetDivisor_StaleReverts(t *testing.T) {
	ctx := context.Background()

	// Fresh timestamp: a divisor-aware announce refreshed it recently, so the
	// count is honored.
	s := newStubSettings()
	setFleetActive(s, 3)
	if got := fleetDivisor(ctx, s); got != 3 {
		t.Errorf("fresh divisor = %d, want 3", got)
	}

	// Stale timestamp (older than the TTL): the control plane stopped refreshing
	// the divisor — member is standalone / detached / under a pre-feature Front
	// Desk — so it reverts to 1 rather than throttle to a frozen 1/3 forever.
	s.set(settingsKeyFleetActiveMembersAt, strconv.FormatInt(time.Now().Add(-fleetDivisorTTL-time.Minute).Unix(), 10))
	if got := fleetDivisor(ctx, s); got != 1 {
		t.Errorf("stale divisor = %d, want reverted 1", got)
	}

	// A live count with no timestamp at all (never refreshed) also reverts to 1.
	s2 := newStubSettings()
	s2.set(settingsKeyFleetActiveMembers, "3")
	if got := fleetDivisor(ctx, s2); got != 1 {
		t.Errorf("no-timestamp divisor = %d, want reverted 1", got)
	}
}
