package proxy

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// dialTiming is the per-attempt slot the SafeDialer writes DNS+TCP time into
// and the attempt reads back. It is atomic because the writer and the reader
// are different goroutines with no ordering between them: http.Transport dials
// on its own goroutine, and that goroutine can outlive the Do call that started
// it. A request cancelled mid-dial returns from Do at once while the dial
// goroutine finishes a moment later and records its time; a spare connection
// the transport raced against an idle one lands the same way. A late write
// after take() is attributed nowhere, which is right: it was not this attempt's
// wait.
type dialTiming struct {
	bits atomic.Uint64
}

// set records the dial time in milliseconds.
func (d *dialTiming) set(ms float64) {
	if d == nil {
		return
	}
	d.bits.Store(math.Float64bits(ms))
}

// take returns the recorded dial time and clears the slot, so an attempt that
// dials more than once (the transient-retry loop) sums the dials it waited for
// and never re-counts one.
func (d *dialTiming) take() float64 {
	if d == nil {
		return 0
	}
	return math.Float64frombits(d.bits.Swap(0))
}

// withDialTiming hands a request context its own timing slot under
// ctxkeys.DialMsKey and returns the slot for the caller to read after Do.
func withDialTiming(ctx context.Context) (context.Context, *dialTiming) {
	dt := &dialTiming{}
	return context.WithValue(ctx, ctxkeys.DialMsKey, dt), dt
}

// recordDialMs is the dialer's side: store the time elapsed since start into
// the request's slot, if the request carries one.
func recordDialMs(ctx context.Context, start time.Time) {
	if dt, ok := ctx.Value(ctxkeys.DialMsKey).(*dialTiming); ok {
		dt.set(float64(time.Since(start).Microseconds()) / 1000.0)
	}
}
