package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// armed fills in the lifetime pieces newDBLogWriter would have created, so a
// test can build a writer as a literal (no run goroutine, a pool of its own)
// and still drive send, flush and stop against it. Without them stop would
// close a nil channel and every flush would derive from a nil context.
func armed(w *dbLogWriter) *dbLogWriter {
	w.stopping = make(chan struct{})
	w.life, w.endLife = context.WithCancel(context.Background())
	return w
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
	w := armed(&dbLogWriter{pool: closedTestPool(t), drops: &logDropReporter{dst: &out, interval: time.Hour}})

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

	// A drop that lands after the final report has no later notice to be
	// carried into: writes racing a shutdown reach the reporter after stop has
	// already said its piece, and throttling them there is silence.
	before := out.String()
	r.drop(1, "after the final report")
	late := strings.TrimPrefix(out.String(), before)
	if !strings.Contains(late, "applog: 1 entries dropped") {
		t.Errorf("a drop after the final report was swallowed by the throttle: %q", late)
	}

	// One notice, not one per entry. reportPending rearms the throttle rather
	// than lifting it, so the drops behind that first late one aggregate the
	// way they always did: a writer stopped while the log path is still busy
	// costs a line per interval, not a line per entry.
	said := out.String()
	r.drop(1, "and another")
	r.drop(1, "and another")
	if out.String() != said {
		t.Errorf("the throttle did not come back after the final report: %q", strings.TrimPrefix(out.String(), said))
	}
	if r.pending != 2 {
		t.Errorf("pending = %d, want 2: the throttled late drops were not kept on the count", r.pending)
	}
}

// And the tail reaches stderr through the shutdown path that actually runs:
// main calls StopAppLogWriter before closing the database, which is what makes
// reportPending more than a method nothing invokes.
func TestDBLogWriter_StopReportsTheSuppressedTail(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	w := armed(&dbLogWriter{
		pool:          closedTestPool(t),
		ch:            make(chan logMsg, 16),
		done:          make(chan struct{}),
		flushInterval: 10 * time.Millisecond,
		sendTimeout:   time.Second,
		drops:         &logDropReporter{dst: &out, interval: time.Hour},
	})
	go w.run()

	entry := func(m string) AppLogEntry {
		return AppLogEntry{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "test", Message: m}
	}
	// First failing flush: reported immediately, and it starts the interval.
	w.ch <- logMsg{entry: entry("one")}
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
	w.ch <- logMsg{entry: entry("two")}
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
	w := armed(&dbLogWriter{
		ch:          make(chan logMsg, 1),
		sendTimeout: 50 * time.Millisecond,
		drops:       &logDropReporter{dst: &out, interval: time.Hour},
	})
	w.ch <- logMsg{entry: AppLogEntry{Message: "occupies the queue"}} // nothing drains it

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
		armed(&dbLogWriter{ch: make(chan logMsg, 1), sendTimeout: 10 * time.Millisecond}),
		armed(&dbLogWriter{ch: make(chan logMsg, 1), sendTimeout: 10 * time.Millisecond, drops: noDst}),
	} {
		w.ch <- logMsg{entry: AppLogEntry{Message: "occupies the queue"}}
		w.write(AppLogEntry{Level: "info", Source: "test", Message: "discarded"})
		w.flush([]AppLogEntry{{Level: "info", Source: "test", Message: "discarded"}})
	}
}

// stop puts one deadline over the whole final drain. When it expires, whatever
// is still queued is dropped in a single counted notice instead of being
// flushed a batch at a time with a deadline each: a queue holding
// dbLogChannelSize entries against a stalled database would otherwise take a
// hundred of those in a row, and the process would be killed with the database
// pool still open.
func TestDBLogWriter_StopDropsWhatTheDeadlineDoesNotCover(t *testing.T) {
	t.Parallel()
	const queued = 20
	var out syncBuffer
	w := armed(&dbLogWriter{
		ch:   make(chan logMsg, queued),
		done: make(chan struct{}),
		// Long enough that only the stop path can move these entries.
		flushInterval: time.Hour,
		sendTimeout:   time.Second,
		drops:         &logDropReporter{dst: &out, interval: time.Hour},
	})
	go w.run()

	for i := range queued {
		w.ch <- logMsg{entry: AppLogEntry{Level: "info", Source: "test", Message: fmt.Sprintf("queued %d", i)}}
	}
	// Stands in for the stop deadline expiring, which is what stop's own timer
	// does to this context.
	w.endLife()

	stopped := make(chan struct{})
	go func() {
		w.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return: the final drain is not bounded by its deadline")
	}

	got := out.String()
	if want := fmt.Sprintf("%d entries dropped", queued); !strings.Contains(got, want) {
		t.Errorf("notice = %q, want one saying %q: the undrained tail has to be accounted, not silently lost", got, want)
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("got %d notices in %q, want exactly one for the whole undrained tail", n, got)
	}
}

// One cause, one notice. The drain writes more than one batch, and a database
// that refuses them is a single incident: reported per batch it reads as
// several, and the operator has to add the counts up by hand to learn how big
// the hole in the history is. Discriminating because the pool here fails every
// Exec, so the drain does hand batches over and does get them back refused, and
// because the reporter throttles nothing (interval 0), so every notice the drain
// produces is visible.
//
// drainTail is driven directly rather than through run: run flushes on its own
// as soon as a batch fills, so a test that fed the queue and then stopped the
// writer would be racing it for the first fifty entries.
func TestDBLogWriter_DrainReportsTheRefusedTailAsOneNotice(t *testing.T) {
	t.Parallel()
	const queued = dbLogBatchSize + 10
	var out syncBuffer
	w := armed(&dbLogWriter{
		pool:  closedTestPool(t),
		ch:    make(chan logMsg, queued),
		drops: &logDropReporter{dst: &out},
	})

	for i := range queued {
		w.ch <- logMsg{entry: AppLogEntry{Level: "info", Source: "test", Message: fmt.Sprintf("queued %d", i)}}
	}
	w.drainTail(nil)

	got := out.String()
	if want := fmt.Sprintf("%d entries dropped", queued); !strings.Contains(got, want) {
		t.Errorf("notice = %q, want one saying %q: the whole refused drain is one hole, counted once", got, want)
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("got %d notices in %q, want exactly one for the whole refused drain", n, got)
	}
}

// A second stop waits for the first one's drain rather than returning on the
// closed flag. Returning early would tell its caller the writer is finished
// while batches are still going into a pool the caller is about to close.
func TestDBLogWriter_SecondStopWaitsForTheFirstDrain(t *testing.T) {
	t.Parallel()
	w := armed(&dbLogWriter{
		ch:            make(chan logMsg, 1),
		done:          make(chan struct{}),
		flushInterval: time.Hour,
		sendTimeout:   time.Second,
		drops:         &logDropReporter{interval: time.Hour},
	})
	// The state a first caller leaves behind while its drain is still running:
	// the queue is retired but done has not been closed yet.
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()

	returned := make(chan struct{})
	go func() {
		w.stop()
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("second stop returned while the first caller's drain was still writing")
	case <-time.After(100 * time.Millisecond):
	}

	close(w.done) // the first caller's drain finishes
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("second stop did not return once the drain had finished")
	}
}

// liveTestPool returns an open pool against the test database.
func liveTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// countAppLogs reports how many rows the given source has.
func countAppLogs(t *testing.T, pool *pgxpool.Pool, source string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM app_logs WHERE source = $1`, source).Scan(&n); err != nil {
		t.Fatalf("count rows for %s: %v", source, err)
	}
	return n
}

// testEntry builds an entry attributed to source.
func testEntry(source, message string) AppLogEntry {
	return AppLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     "info",
		Source:    source,
		Message:   message,
	}
}

// Shutdown used to close the queue under the producers and rely on a recover in
// write to absorb the "send on closed channel" that followed, which is a panic
// caught after the fact, not synchronisation: whether it fired at all depended
// on the schedule. Writers racing stop must simply never panic.
func TestDBLogWriter_ConcurrentWriteDuringStopNeverPanics(t *testing.T) {
	pool := liveTestPool(t)
	const source = "stop-race-test"
	if _, err := pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = $1`, source); err != nil {
		t.Fatalf("clean: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = $1`, source)
	})

	w := newDBLogWriter(pool, 10*time.Millisecond)

	// The other half of the guarantee, on the racing path this time: a send
	// that answered nil while the close was already under way is a row, not a
	// maybe. Only send reports that, which is why the writers call it directly;
	// write is send plus the drop accounting.
	var mu sync.Mutex
	var accepted []string
	// Every writer reports its first accepted send, so stop lands on a queue
	// that already holds entries rather than possibly ahead of all of them.
	running := make(chan struct{}, 8)
	var writers sync.WaitGroup
	for g := range 8 {
		writers.Go(func() {
			var mine []string
			for i := range 50 {
				msg := fmt.Sprintf("g%d-%d", g, i)
				timer := time.NewTimer(w.sendTimeout)
				err := w.send(logMsg{entry: testEntry(source, msg)}, timer.C)
				timer.Stop()
				if err == nil {
					mine = append(mine, msg)
				}
				if i == 0 {
					running <- struct{}{}
				}
			}
			mu.Lock()
			accepted = append(accepted, mine...)
			mu.Unlock()
		})
	}
	for range 8 {
		<-running
	}
	// Stop while the writers above are still going: the whole point is that the
	// close lands in the middle of concurrent sends.
	w.stop()
	writers.Wait()

	// A write after stop is a drop, not a panic, and not a resurrected queue.
	w.write(testEntry(source, "after stop"))

	persisted := appLogMessages(t, pool, source)
	if len(accepted) == 0 {
		t.Fatal("no send was accepted, so the race never happened and nothing is being proved")
	}
	for _, msg := range accepted {
		if !persisted[msg] {
			t.Fatalf("%q was accepted by the writer but never reached the database: a send that returned nil must be a row", msg)
		}
	}
	if persisted["after stop"] {
		t.Fatal("an entry written after stop reached the database")
	}
}

// appLogMessages reports which messages the given source has rows for.
func appLogMessages(t *testing.T, pool *pgxpool.Pool, source string) map[string]bool {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT message FROM app_logs WHERE source = $1`, source)
	if err != nil {
		t.Fatalf("read rows for %s: %v", source, err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			t.Fatalf("scan row for %s: %v", source, err)
		}
		seen[msg] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows for %s: %v", source, err)
	}
	return seen
}

// The hot log path is bounded by sendTimeout and nothing else. RWMutex parks a
// new reader behind a waiting writer, so a sender that took the read lock
// before consulting the closed state would be held for the whole of another
// sender's send whenever stop was queued between them, with its own deadline
// not yet in the select.
func TestDBLogWriter_WriteStaysBoundedWhileStopWaits(t *testing.T) {
	const sendTimeout = 50 * time.Millisecond
	w := armed(&dbLogWriter{
		ch:          make(chan logMsg, 1),
		done:        make(chan struct{}),
		sendTimeout: sendTimeout,
		drops:       &logDropReporter{dst: &syncBuffer{}, interval: time.Hour},
	})
	// No run goroutine: stop only has to get past the senders, and the queue
	// stays full so a sender that reaches the send blocks in it.
	close(w.done)
	w.ch <- logMsg{}

	// A sender that holds the read lock for two seconds.
	const hold = 2 * time.Second
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		_ = w.send(logMsg{entry: testEntry("bound-test", "holder")}, time.After(hold))
	}()
	time.Sleep(100 * time.Millisecond)

	// stop now queues for the write lock behind that sender.
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		w.stop()
	}()
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	w.write(testEntry("bound-test", "measured"))
	elapsed := time.Since(start)

	<-sent
	<-stopped
	if elapsed > 10*sendTimeout {
		t.Fatalf("write took %s with stop waiting, want at most its own %s send timeout", elapsed, sendTimeout)
	}
}

// The other half of the same guarantee: an entry whose write returned before
// stop was called reaches the database, so shutdown is a flush and not a
// truncation.
func TestDBLogWriter_StopPersistsEverythingWrittenBefore(t *testing.T) {
	pool := liveTestPool(t)
	const source = "stop-flush-test"
	if _, err := pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = $1`, source); err != nil {
		t.Fatalf("clean: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = $1`, source)
	})

	// An interval long enough that nothing flushes on the ticker: only stop can
	// have written these rows.
	w := newDBLogWriter(pool, time.Hour)
	const want = 20
	for i := range want {
		w.write(testEntry(source, fmt.Sprintf("entry %d", i)))
	}
	w.stop()

	if got := countAppLogs(t, pool, source); got != want {
		t.Fatalf("rows after stop = %d, want %d: entries written before stop were lost", got, want)
	}
}

// The barrier is what lets a purge own the rows: everything queued before it
// has to be in the database by the time it returns.
func TestDBLogWriter_FlushBarrierDrainsQueuedEntries(t *testing.T) {
	pool := liveTestPool(t)
	const source = "barrier-test"
	if _, err := pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = $1`, source); err != nil {
		t.Fatalf("clean: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = $1`, source)
	})

	// A ticker that will not fire during the test, so the barrier is the only
	// thing that can have flushed the queue.
	w := newDBLogWriter(pool, time.Hour)
	defer w.stop()
	for i := range 5 {
		w.write(testEntry(source, fmt.Sprintf("queued %d", i)))
	}

	if err := w.flushBarrier(appLogFlushBarrierTimeout); err != nil {
		t.Fatalf("flushBarrier: %v", err)
	}
	if got := countAppLogs(t, pool, source); got != 5 {
		t.Fatalf("rows after barrier = %d, want 5: the barrier returned with entries still queued", got)
	}
}

// A barrier after shutdown says so instead of blocking for its whole budget or
// sending on a closed channel.
func TestDBLogWriter_FlushBarrierAfterStop(t *testing.T) {
	w := newDBLogWriter(nil, time.Hour)
	w.stop()
	if err := w.flushBarrier(appLogFlushBarrierTimeout); !errors.Is(err, errLogWriterStopped) {
		t.Fatalf("flushBarrier after stop = %v, want %v", err, errLogWriterStopped)
	}
	// stop is idempotent: shutdown paths may reach it twice.
	w.stop()
}

// A barrier that gets into the queue but is never drained gives up on its own
// budget rather than holding the operator's purge open indefinitely.
func TestDBLogWriter_FlushBarrierGivesUp(t *testing.T) {
	t.Parallel()
	// No run goroutine: the barrier is enqueued and nothing ever answers it.
	w := armed(&dbLogWriter{ch: make(chan logMsg, 1), sendTimeout: time.Second})
	if err := w.flushBarrier(20 * time.Millisecond); !errors.Is(err, errLogWriterFlushSlow) {
		t.Fatalf("flushBarrier with no writer running = %v, want %v", err, errLogWriterFlushSlow)
	}
}

// With no writer configured there is nothing to drain, which is not an error:
// ClearAppLogs runs this on every purge, DB or not.
func TestFlushAppLogWriter_NoWriter(t *testing.T) {
	saved := dbWriter.Load()
	dbWriter.Store(nil)
	defer func() { dbWriter.Store(saved) }()
	if err := flushAppLogWriter(appLogFlushBarrierTimeout); err != nil {
		t.Fatalf("flushAppLogWriter with no writer = %v, want nil", err)
	}
}
