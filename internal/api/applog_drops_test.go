package api

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// syncBuffer is a capture destination safe to read while the writer goroutine
// is still writing to it. A plain bytes.Buffer here is a data race, and the
// race is the point of one of the tests below.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// closedTestPool returns a pool whose every Exec fails, for driving the
// batch-insert failure path.
func closedTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	pool.Close()
	return pool
}

// A batch the database refused used to vanish with `_ = err`: up to 50 entries
// gone from the App Logs history with no evidence anywhere, which reads exactly
// like the gateway never having logged at all.
func TestDBLogWriter_ReportsEntriesItCouldNotPersist(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	// Built as a literal rather than through newDBLogWriter: flush needs only
	// the pool and the reporter, and this way the capture destination is set
	// before anything could read it, with no run goroutine in the picture.
	w := &dbLogWriter{pool: closedTestPool(t), drops: &logDropReporter{dst: &out, interval: time.Hour}}

	w.flush([]AppLogEntry{
		{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: "first"},
		{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: "second"},
		{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: "third"},
	})

	got := out.String()
	if got == "" {
		t.Fatal("a batch was discarded with no notice anywhere")
	}
	// Anchored to the start of the notice: "3 entries dropped" is itself a
	// substring of "33 entries dropped", so only the prefix pins the count.
	if !strings.Contains(got, "applog: 3 entries dropped") {
		t.Errorf("notice does not say how many were lost: %q", got)
	}
	// The reason has to be actionable, not just "something failed".
	if !strings.Contains(got, "closed") {
		t.Errorf("notice does not carry the database's own reason: %q", got)
	}
	for _, msg := range []string{"first", "second", "third"} {
		if strings.Contains(got, msg) {
			t.Errorf("notice echoes log content %q: %q", msg, got)
		}
	}
}

// The condition that causes drops repeats on every flush, so the notice has to
// be throttled — but throttled is not the same as lost: the entries suppressed
// in between are still counted, and still reported.
func TestLogDropReporter_ThrottlesWithoutLosingTheCount(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	r := &logDropReporter{dst: &out, interval: time.Hour}

	r.drop(3, "first failure")
	first := out.String()
	if !strings.Contains(first, "applog: 3 entries dropped") {
		t.Fatalf("the first drop must be reported at once, got %q", first)
	}

	r.drop(5, "second failure")
	r.drop(2, "third failure")
	if out.String() != first {
		t.Errorf("drops inside the interval were not throttled: %q", out.String())
	}

	r.reportPending()
	tail := strings.TrimPrefix(out.String(), first)
	if !strings.Contains(tail, "applog: 7 entries dropped") {
		t.Errorf("the 7 suppressed entries were never accounted for: %q", tail)
	}
}

// And the tail reaches stderr through the shutdown path that actually runs:
// main calls StopAppLogWriter before closing the database, which is what makes
// reportPending more than a method nothing invokes.
func TestDBLogWriter_StopReportsTheSuppressedTail(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	w := &dbLogWriter{
		pool:          closedTestPool(t),
		ch:            make(chan AppLogEntry, 16),
		done:          make(chan struct{}),
		flushInterval: 10 * time.Millisecond,
		sendTimeout:   time.Second,
		drops:         &logDropReporter{dst: &out, interval: time.Hour},
	}
	go w.run()

	entry := func(m string) AppLogEntry {
		return AppLogEntry{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: m}
	}
	// First failing flush: reported immediately, and it starts the interval.
	w.ch <- entry("one")
	deadline := time.Now().Add(5 * time.Second)
	for out.String() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	firstNotice := out.String()
	if firstNotice == "" {
		t.Fatal("the first failing flush was never reported")
	}

	// Second one is inside the interval, so it is suppressed and must survive
	// only on the pending count.
	w.ch <- entry("two")
	w.stop()

	tail := strings.TrimPrefix(out.String(), firstNotice)
	if !strings.Contains(tail, "(final)") {
		t.Errorf("shutdown swallowed the throttled tail: %q", tail)
	}
}

// The other silent door: the writer's queue is full and the entry is discarded
// rather than blocking the caller.
func TestDBLogWriter_ReportsAnEntryTheQueueCouldNotTake(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	w := &dbLogWriter{
		ch:          make(chan AppLogEntry, 1),
		sendTimeout: 50 * time.Millisecond,
		drops:       &logDropReporter{dst: &out, interval: time.Hour},
	}
	w.ch <- AppLogEntry{Message: "occupies the queue"} // nothing drains it

	w.write(AppLogEntry{Level: "info", Source: "test", Message: "dropped-entry-text"})

	got := out.String()
	if got == "" {
		t.Fatal("an entry was dropped from a full queue with no notice")
	}
	if !strings.Contains(got, "applog: 1 entries dropped") {
		t.Errorf("notice does not say how many were lost: %q", got)
	}
	if strings.Contains(got, "dropped-entry-text") {
		t.Errorf("notice echoes log content: %q", got)
	}
}

// A writer built the normal way reports to STDERR and uses the real timeout.
//
// The destination is the assertion that matters: point it at io.Discard and
// every other test here still passes while production is silently dropping
// entries again, which is the whole bug.
func TestNewDBLogWriter_ProductionDefaults(t *testing.T) {
	t.Parallel()
	w := newDBLogWriter(nil, dbLogFlushInterval)
	defer w.stop()
	if w.sendTimeout != dbLogSendTimeout {
		t.Errorf("sendTimeout = %s, want %s", w.sendTimeout, dbLogSendTimeout)
	}
	if w.drops == nil {
		t.Fatal("a writer with no drop reporter discards entries silently again")
	}
	if w.drops.dst != os.Stderr {
		t.Errorf("drop notices go to %T, not os.Stderr: nothing would ever see them", w.drops.dst)
	}
	if w.drops.interval != dbLogDropReportInterval {
		t.Errorf("report interval = %s, want %s", w.drops.interval, dbLogDropReportInterval)
	}
}

// A reporter that is missing, or has nowhere to write, must not take the
// process down. flush runs on the run goroutine, which has no recover of its
// own, so a panic there would kill the server over a missing diagnostic.
func TestLogDropReporter_NilIsInert(t *testing.T) {
	t.Parallel()
	var nilReporter *logDropReporter
	nilReporter.drop(5, "no reporter attached")
	nilReporter.reportPending()

	noDst := &logDropReporter{interval: time.Hour}
	noDst.drop(5, "reporter with nowhere to write")
	noDst.reportPending()

	for _, w := range []*dbLogWriter{
		{ch: make(chan AppLogEntry, 1), sendTimeout: 10 * time.Millisecond},
		{ch: make(chan AppLogEntry, 1), sendTimeout: 10 * time.Millisecond, drops: noDst},
	} {
		w.ch <- AppLogEntry{Message: "occupies the queue"}
		w.write(AppLogEntry{Level: "info", Source: "test", Message: "discarded"})
		w.flush([]AppLogEntry{{Level: "info", Source: "test", Message: "discarded"}})
	}
}
