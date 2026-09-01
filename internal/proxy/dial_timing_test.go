package proxy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// The slot's contract: set/take round-trips, take clears, a nil slot is
// inert, and a context without a slot is ignored by the dialer's recorder.
func TestDialTiming_SetTakeClears(t *testing.T) {
	var slot *dialTiming
	slot.set(1)
	if slot.take() != 0 {
		t.Fatal("nil slot returned a value")
	}
	ctx, dt := withDialTiming(context.Background())
	if _, ok := ctx.Value(ctxkeys.DialMsKey).(*dialTiming); !ok {
		t.Fatal("the context does not carry the slot under DialMsKey")
	}
	dt.set(12.5)
	if got := dt.take(); got != 12.5 {
		t.Errorf("take = %v, want 12.5", got)
	}
	if got := dt.take(); got != 0 {
		t.Errorf("second take = %v, want the slot cleared", got)
	}
	recordDialMs(context.Background(), time.Now()) // no slot: nothing to do, no panic
	recordDialMs(ctx, time.Now().Add(-40*time.Millisecond))
	if got := dt.take(); got < 40 {
		t.Errorf("recorded %v ms, want at least the 40ms elapsed", got)
	}
}

// The shape the race detector caught on master: the transport's dial
// goroutine records its time after the request goroutine has already moved
// on to read the slot. With a plain pointer that is a data race; the slot
// makes both sides safe whichever lands first, and a late write is simply
// not this attempt's wait.
func TestDialTiming_LateDialWriteDoesNotRaceTheReader(t *testing.T) {
	ctx, dt := withDialTiming(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			recordDialMs(ctx, time.Now())
		}()
		go func() {
			defer wg.Done()
			_ = dt.take()
		}()
	}
	wg.Wait()
}
