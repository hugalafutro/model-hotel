package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gatedSettings blocks GetFloat while hold is closed-over open, so a test can
// park one admission mid-snapshot and let another install a newer cap.
type gatedSettings struct {
	*stubSettings
	hold    chan struct{}
	parked  atomic.Bool // only the first reader parks; a sync.Once would park the second too
	entered chan struct{}
}

func (g *gatedSettings) GetFloat(ctx context.Context, key string, def float64) float64 {
	v := g.stubSettings.GetFloat(ctx, key, def) // the snapshot, taken before the park
	if g.parked.CompareAndSwap(false, true) {
		close(g.entered)
		<-g.hold
	}
	return v
}

// A request that resolved the old cap before a lower one was installed must
// not write the old cap back over the new one. The snapshot is taken under
// the admission mutex, so the delayed request either finishes first (and the
// newer request then lowers the cap) or waits behind the newer one and sees
// the lowered settings itself; either way the lowered cap is what stands.
func TestGetLimiter_LoweredCapIsNotRestoredByAStaleSnapshot(t *testing.T) {
	stub := newStubSettings()
	stub.set(settingsKeyRPS, "100")
	stub.set(settingsKeyBurst, "100")
	g := &gatedSettings{stubSettings: stub, hold: make(chan struct{}), entered: make(chan struct{})}
	lim := NewLimiter(g)
	defer lim.Stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the stale request: parks inside its settings read
		defer wg.Done()
		lim.getLimiter(context.Background(), "k", nil, nil)
	}()
	<-g.entered

	stub.set(settingsKeyRPS, "1")
	stub.set(settingsKeyBurst, "1")
	wg.Add(1)
	go func() { // the newer request, carrying the lowered cap
		defer wg.Done()
		lim.getLimiter(context.Background(), "k", nil, nil)
	}()
	time.Sleep(50 * time.Millisecond) // let it reach the mutex
	close(g.hold)
	wg.Wait()

	lim.mu.Lock()
	entry := lim.limiters["k"]
	lim.mu.Unlock()
	if entry == nil || entry.rps != 1 || entry.burst != 1 {
		t.Fatalf("lowered cap was overwritten by the stale snapshot: rps=%v burst=%v", entry.rps, entry.burst)
	}
}
