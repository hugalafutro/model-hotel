package api

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A batch the database refused used to vanish with `_ = err`: up to 50 entries
// gone from the App Logs history with no evidence anywhere, which reads exactly
// like the gateway never having logged at all. That is what made a filtered
// docker-logs surface look like a stack "dropping INFO logs".
func TestDBLogWriter_ReportsEntriesItCouldNotPersist(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	pool.Close() // every Exec from here on fails

	var out bytes.Buffer
	w := newDBLogWriter(pool, dbLogFlushInterval)
	defer w.stop()
	w.drops.dst = &out

	w.flush([]AppLogEntry{
		{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: "first"},
		{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: "second"},
		{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: "third"},
	})

	got := out.String()
	if got == "" {
		t.Fatal("a batch was discarded with no notice anywhere")
	}
	if !strings.Contains(got, "3") {
		t.Errorf("notice does not say how many were lost: %q", got)
	}
	// The reason has to be actionable, not just "something failed".
	if !strings.Contains(got, "closed") {
		t.Errorf("notice does not carry the database's own reason: %q", got)
	}
	// The entries themselves are not repeated into the notice.
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
	var out bytes.Buffer
	r := &logDropReporter{dst: &out, interval: time.Hour}

	r.drop(3, "first failure")
	first := out.String()
	if !strings.Contains(first, "3") {
		t.Fatalf("the first drop must be reported at once, got %q", first)
	}

	r.drop(5, "second failure")
	r.drop(2, "third failure")
	if out.String() != first {
		t.Errorf("drops inside the interval were not throttled: %q", out.String())
	}

	// Shutdown must not swallow what the throttle was holding.
	r.reportPending()
	tail := strings.TrimPrefix(out.String(), first)
	if !strings.Contains(tail, "7") {
		t.Errorf("the 7 suppressed entries were never accounted for: %q", tail)
	}
}

// The other silent door: the writer's queue is full and the entry is discarded
// rather than blocking the caller.
func TestDBLogWriter_ReportsAnEntryTheQueueCouldNotTake(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
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
	if !strings.Contains(got, "1") {
		t.Errorf("notice does not say how many were lost: %q", got)
	}
	if strings.Contains(got, "dropped-entry-text") {
		t.Errorf("notice echoes log content: %q", got)
	}
}

// A writer built the normal way reports to stderr and uses the real timeout;
// nothing here is wired only for the tests above.
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
	if w.drops.interval != dbLogDropReportInterval {
		t.Errorf("report interval = %s, want %s", w.drops.interval, dbLogDropReportInterval)
	}
}

// A writer assembled without a reporter still works. The nil check is not
// decoration: dbLogWriter is built as a struct literal in several tests, and a
// dropped entry must not take the whole process down over a missing diagnostic.
func TestLogDropReporter_NilIsInert(t *testing.T) {
	t.Parallel()
	var r *logDropReporter
	r.drop(5, "no reporter attached")
	r.reportPending()

	w := &dbLogWriter{ch: make(chan AppLogEntry, 1), sendTimeout: 10 * time.Millisecond}
	w.ch <- AppLogEntry{Message: "occupies the queue"}
	w.write(AppLogEntry{Level: "info", Source: "test", Message: "discarded"})
}
