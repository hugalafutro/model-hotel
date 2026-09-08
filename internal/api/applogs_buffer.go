package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// stderrLogFilter is an io.Writer that forwards log lines to an underlying
// writer (os.Stderr). When DEBUG_LOG is enabled, all levels are forwarded so
// docker logs show the full picture. Otherwise only errors and warnings are
// forwarded to keep docker logs clean while the ring buffer still captures
// everything for the DB.
//
// Sources listed in stderrSuppressSources are completely suppressed from
// docker logs (all levels). This is useful for noisy sources whose errors
// are not operationally useful in docker logs but still need to be captured
// in the database for full visibility in the app UI. Add sources here when
// you decide certain errors should not clutter docker logs.
type stderrLogFilter struct {
	dst io.Writer
}

// stderrSuppressSources is the set of log sources that should be completely
// suppressed from docker logs (stderr), regardless of level. Entries from
// these sources still flow to the ring buffer and database for full visibility
// in the app UI. Start empty — add sources when you decide their errors
// should not appear in docker logs.
var stderrSuppressSources = map[string]bool{}

func (f *stderrLogFilter) Write(p []byte) (n int, err error) {
	text := string(p)
	debugEnabled := debuglog.Level() <= slog.LevelDebug
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		src, lvl, _ := parseLogLine(line)
		if stderrSuppressSources[src] {
			continue
		}
		if debugEnabled || lvl == "error" || lvl == "warning" {
			if _, err := f.dst.Write([]byte(line + "\n")); err != nil {
				return 0, err
			}
		}
	}
	return len(p), nil
}

const appLogBufferSize = 500

// appLogCountCache caches unfiltered level/source counts with a short TTL.
// Pill badge counts don't need to be real-time — a few seconds of staleness is fine.
var (
	appLogCountCache struct {
		sync.RWMutex
		levelCounts  map[string]int
		sourceCounts map[string]int
		fetchedAt    time.Time
	}
	appLogCountCacheTTL = 5 * time.Second
)

// invalidateAppLogCountCache drops the cached level/source counts so the next
// getAppLogCounts re-queries the DB. Call it after any operation that changes the
// app_logs row set out of band (e.g. ClearAppLogs): the unfiltered total is
// derived from these counts, so without this a poll within appLogCountCacheTTL
// would keep reporting the pre-change (stale) total.
func invalidateAppLogCountCache() {
	appLogCountCache.Lock()
	appLogCountCache.levelCounts = nil
	appLogCountCache.sourceCounts = nil
	appLogCountCache.fetchedAt = time.Time{}
	appLogCountCache.Unlock()
}

// appLogBuffer is the global ring buffer that captures log output.
var appLogBuffer *ringBuffer

// dbWriter is the asynchronous database log writer (nil if no pool). Atomic
// because the producers read it from request and logging goroutines while
// InitAppLogBuffer sets it, and every reader's decision (write the entry, hold
// the purge behind a barrier) depends on which value it observes. It is set
// once and never cleared: a stopped writer stays here so a producer arriving
// after shutdown is refused and counted rather than seeing "no writer at all"
// and dropping the entry silently.
var dbWriter atomic.Pointer[dbLogWriter]

// ringBuffer is a fixed-size circular buffer of AppLogEntry values.
type ringBuffer struct {
	mu      sync.RWMutex
	entries []AppLogEntry
	head    int // next write position
	count   int // number of entries written (up to capacity)
}

// dbLogChannelSize is the buffered channel capacity for the async DB log
// writer. At ~200 log lines/sec throughput with 50-row batches flushed every
// 500ms, a buffer of 5000 can absorb ~25 seconds of DB unavailability before
// backpressure is applied to the caller.
const dbLogChannelSize = 5000

// dbLogSendTimeout is how long the DB log writer will block trying to enqueue
// an entry before giving up. This prevents a slow or unreachable database
// from stalling the hot path (log.Printf) indefinitely.
const dbLogSendTimeout = 5 * time.Second

// dbLogBatchSize is how many entries accumulate before the writer flushes them
// as one INSERT.
const dbLogBatchSize = 50

// dbLogFlushTimeout bounds one batch INSERT.
const dbLogFlushTimeout = 5 * time.Second

// stopDrainTimeout bounds StopAppLogWriter end to end: the wait for the senders
// already in flight, a flush already under way and the final drain of what is
// still queued all share it. It has to be one deadline over all three, because
// the queue holds up to dbLogChannelSize entries and draining them a batch at a
// time with a deadline each would let a stalled database hold shutdown for a
// hundred of those in a row, past any container grace period and with the
// database pool never closed. What does not fit is counted as dropped instead.
const stopDrainTimeout = 5 * time.Second

// logMsg is what travels the writer's queue: an entry to persist, or a flush
// barrier when flushed is non-nil. Barriers ride the same channel as entries so
// everything queued ahead of one is written before it completes; on a channel
// of their own they would be selected in arbitrary order and could complete
// while entries were still in flight.
type logMsg struct {
	entry   AppLogEntry
	flushed chan struct{}
}

// Why an entry never reached the database, as the reasons a caller has to tell
// apart: a stopped writer is shutdown, a full queue is a database that has been
// unreachable long enough to back the queue up, and a slow flush is a barrier
// that ran out of budget waiting for the rows already queued.
var (
	errLogWriterStopped   = errors.New("writer stopped")
	errLogWriterQueueFull = errors.New("writer queue full")
	errLogWriterFlushSlow = errors.New("writer flush did not finish in time")
)

type dbLogWriter struct {
	pool          *pgxpool.Pool
	ch            chan logMsg
	done          chan struct{}
	flushInterval time.Duration
	// stopping closes when stop begins, which is what tells run to drain what
	// is queued and return. The queue channel itself is never closed, so a send
	// racing the stop cannot panic whatever else changes around it.
	stopping chan struct{}
	// life is the parent of every flush deadline, and a sender parked on a full
	// queue watches it too. stop cancels it once stopDrainTimeout is up, so that
	// single deadline ends all three at once instead of each carrying its own.
	life    context.Context
	endLife context.CancelFunc
	// mu guards closed and, with it, every send on ch. A sender holds the read
	// lock across its send and stop takes the write lock before retiring the
	// queue, so an entry is either refused or queued for a run goroutine that is
	// still there to drain it, and no recover stands in for the synchronisation.
	mu     sync.RWMutex
	closed bool
	// sendTimeout is how long write blocks before discarding an entry, always
	// dbLogSendTimeout in production. A field purely so the queue-full test can
	// prove the drop in milliseconds instead of sitting out the real five
	// seconds; nothing else varies it, and no race requires it — write runs on
	// the CALLER's goroutine, and the run goroutine reads only flushInterval.
	sendTimeout time.Duration
	drops       *logDropReporter
}

// dbLogDropReportInterval throttles the drop notice. The condition that causes
// drops — a database that is unreachable or refusing the batch — recurs on every
// flush, so an unthrottled notice would become the flood it is reporting.
const dbLogDropReportInterval = 10 * time.Second

// logDropReporter accounts for app-log entries that never reached the database
// and says so on stderr.
//
// Straight to stderr, never through debuglog: a notice about the log writer
// would route back into the log writer and recurse. That is why the flush error
// was swallowed outright — but swallowing it loses up to a whole batch from the
// App Logs history with no evidence anywhere, which is indistinguishable from
// the gateway never having logged at all. The ring buffer is not a substitute:
// it holds appLogBufferSize entries for the live view, not the history.
//
// Only the count and the database's own reason are printed. A pgx PgError
// renders as severity, message and SQLSTATE — never its DETAIL — so a rejected
// row's contents are not echoed into the notice.
type logDropReporter struct {
	dst        io.Writer
	interval   time.Duration
	mu         sync.Mutex
	pending    int
	lastReport time.Time
}

// drop records n dropped entries, reporting them unless the last report was
// inside the interval. Suppressed entries stay on the count and are carried into
// the next report, or into reportPending at shutdown.
//
// The count covers every drop since the last notice; the reason is the most
// recent one, which is why it is labelled as such rather than presented as the
// cause of all of them. "not persisted" is also the confident reading of an
// ambiguous case — a deadline that expires after the server committed but before
// the reply arrives reports rows that did in fact land — but over-reporting a
// hole is the right default when the alternative is silence.
func (r *logDropReporter) drop(n int, reason string) {
	if r == nil || r.dst == nil || n <= 0 {
		return
	}
	// Held across the write. drop is called from the run goroutine (flush) and
	// from arbitrary callers (write), and a notice is at most one per interval,
	// so there is nothing to gain by releasing early and a interleaved line to
	// lose. os.Stderr survives concurrent writes; an io.Writer in general does
	// not, and this type carries a mutex precisely so it need not care which.
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending += n
	now := time.Now()
	// No IsZero special case: the zero Time is far enough in the past that Sub
	// saturates well past any interval, so the first drop always reports. That
	// is also how the drops after reportPending are handled: it rearms the
	// throttle to the zero Time rather than lifting it, so the first of them is
	// said at once and the rest still aggregate on the interval.
	if now.Sub(r.lastReport) < r.interval {
		return
	}
	r.lastReport = now
	dropped := r.pending
	r.pending = 0
	_, _ = fmt.Fprintf(r.dst, "applog: %d entries dropped, not persisted (most recent: %s)\n", dropped, reason)
}

// reportPending states whatever the throttle is still holding, so a shutdown
// during an outage does not swallow the tail of it. It is the counter's last
// scheduled reader, so it rearms the throttle on the way out: the next drop
// reports itself immediately, having no later notice to be carried into, and
// the ones behind it are throttled as usual. Lifting the throttle outright
// instead would give a writer stopped while the log path is still busy one
// stderr line per entry.
func (r *logDropReporter) reportPending() {
	if r == nil || r.dst == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastReport = time.Time{}
	if r.pending == 0 {
		return
	}
	dropped := r.pending
	r.pending = 0
	_, _ = fmt.Fprintf(r.dst, "applog: %d entries dropped, not persisted (final)\n", dropped)
}

// newDBLogWriter starts a writer that drains a partial batch every
// flushInterval. The interval is a parameter rather than a package var a test
// can shrink: the writer goroutine reads it while another test is assigning to
// it, which the race detector reports (and which is a real race, not a test
// artifact, because the goroutine outlives the test that started it).
func newDBLogWriter(pool *pgxpool.Pool, flushInterval time.Duration) *dbLogWriter {
	life, endLife := context.WithCancel(context.Background())
	w := &dbLogWriter{
		pool:          pool,
		ch:            make(chan logMsg, dbLogChannelSize),
		done:          make(chan struct{}),
		flushInterval: flushInterval,
		stopping:      make(chan struct{}),
		life:          life,
		endLife:       endLife,
		sendTimeout:   dbLogSendTimeout,
		drops:         &logDropReporter{dst: os.Stderr, interval: dbLogDropReportInterval},
	}
	go w.run()
	return w
}

// dbLogFlushInterval is how often the writer flushes a partial batch.
const dbLogFlushInterval = 500 * time.Millisecond

func (w *dbLogWriter) run() {
	batch := make([]AppLogEntry, 0, dbLogBatchSize)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case msg := <-w.ch:
			if msg.flushed != nil {
				// A barrier: everything queued before it is in batch, so
				// writing it out now is what the waiter is waiting for.
				if len(batch) > 0 {
					w.flush(batch)
					batch = batch[:0]
				}
				close(msg.flushed)
				continue
			}
			batch = append(batch, msg.entry)
			if len(batch) >= dbLogBatchSize {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-w.stopping:
			w.drainTail(batch)
			close(w.done)
			return
		}
	}
}

// drainTail writes out whatever is left when stop begins and accounts the rest
// in one notice. Everything here runs under w.life, which stop cancels at
// stopDrainTimeout, so a queue too deep or a database too slow to finish inside
// that costs a single counted line rather than a shutdown that overruns its
// grace period.
//
// A flush barrier still in the queue is passed over rather than closed: the
// purge waiting on it must not be told the queue is drained by a writer that is
// about to drop part of it. Its own timeout answers it instead.
//
// The whole drain accounts for itself in ONE notice. A batch the database
// refused and a batch the deadline never let us hand over are the same hole in
// the history from the reader's side, and reporting them separately turns one
// cause into two lines that look like two incidents. So the batches written
// here go through insert rather than flush (which reports on its own) and this
// keeps the running total.
func (w *dbLogWriter) drainTail(batch []AppLogEntry) {
	refused := 0
	reason := "shutdown drain exceeded " + stopDrainTimeout.String()
	// Writes batch out unless the stop deadline has already expired. Once it
	// has, batch is left alone so the count below covers it under the deadline
	// that actually dropped it, instead of an INSERT on a cancelled context
	// reporting it as a database failure first.
	writeOut := func() {
		if w.life.Err() != nil || len(batch) == 0 {
			return
		}
		if err := w.insert(batch); err != nil {
			refused += len(batch)
			reason = "batch insert failed: " + err.Error()
		}
		batch = batch[:0]
	}

drain:
	for w.life.Err() == nil {
		select {
		case msg := <-w.ch:
			if msg.flushed != nil {
				continue
			}
			batch = append(batch, msg.entry)
			if len(batch) >= dbLogBatchSize {
				writeOut()
			}
		default:
			break drain
		}
	}
	writeOut()
	// What the queue still holds is drained rather than measured with len(w.ch):
	// the channel carries flush barriers as well as entries, and counting its
	// depth would report a queued barrier as a lost log line. A barrier left here
	// is answered by its own sender's timeout, which is what bounds a purge that
	// arrives during shutdown.
	stranded := 0
	for {
		select {
		case msg := <-w.ch:
			if msg.flushed == nil {
				stranded++
			}
		default:
			if left := refused + len(batch) + stranded; left > 0 {
				w.drops.drop(left, reason)
			}
			return
		}
	}
}

// flush writes one batch and accounts for it if the database refused it. The
// shutdown drain uses insert directly instead, so its whole tail is one notice.
func (w *dbLogWriter) flush(entries []AppLogEntry) {
	if err := w.insert(entries); err != nil {
		w.drops.drop(len(entries), "batch insert failed: "+err.Error())
	}
}

// insert writes one batch as a single INSERT and returns what the database
// said. It never logs: a notice about the log writer routed through debuglog
// would come straight back into this writer and recurse, which is why the
// reporter writes to stderr directly.
func (w *dbLogWriter) insert(entries []AppLogEntry) error {
	if w.pool == nil || len(entries) == 0 {
		return nil
	}
	// Derived from w.life rather than from Background, so the stop deadline ends
	// a flush already in flight instead of adding its own five seconds on top.
	ctx, cancel := context.WithTimeout(w.life, dbLogFlushTimeout)
	defer cancel()

	// Build batch INSERT
	builder := strings.Builder{}
	builder.WriteString("INSERT INTO app_logs (timestamp, level, source, message, escaped, attrs_at) VALUES ")
	args := make([]any, 0, len(entries)*6)
	for i, e := range entries {
		if i > 0 {
			builder.WriteString(", ")
		}
		offset := i * 6
		fmt.Fprintf(&builder, "($%d, $%d, $%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4, offset+5, offset+6)
		args = append(args, e.Timestamp, e.Level, e.Source, e.Message, e.Escaped, e.AttrsAt)
	}
	_, err := w.pool.Exec(ctx, builder.String(), args...)
	return err
}

// send hands msg to the run goroutine, giving up when giveUp fires. The read
// lock is what makes it safe: stop takes the write lock before it retires the
// queue, so a sender that got past the closed check is one stop waits out.
//
// TryRLock rather than RLock, because RWMutex parks a new reader behind a
// waiting writer: with a plain RLock, a caller arriving while stop is queued
// behind another sender's send would wait out that whole send before its own
// deadline was even in the select, and the hot log path would no longer be
// bounded by its sendTimeout. TryRLock fails exactly when stop holds or is
// waiting for the write lock, which is the answer this caller wants anyway.
func (w *dbLogWriter) send(msg logMsg, giveUp <-chan time.Time) error {
	if !w.mu.TryRLock() {
		return errLogWriterStopped
	}
	defer w.mu.RUnlock()
	if w.closed {
		return errLogWriterStopped
	}
	select {
	case w.ch <- msg:
		return nil
	case <-w.life.Done():
		// The stop deadline expired while this sender was parked on a full
		// queue. Refused here rather than left holding the read lock, so what
		// bounds stop is that deadline and not this sender's own.
		return errLogWriterStopped
	case <-giveUp:
		return errLogWriterQueueFull
	}
}

func (w *dbLogWriter) write(entry AppLogEntry) {
	timer := time.NewTimer(w.sendTimeout)
	defer timer.Stop()
	err := w.send(logMsg{entry: entry}, timer.C)
	if err == nil {
		return
	}
	// DB writer is backed up, or already stopped — drop the entry rather than
	// blocking the caller. The ring buffer still has it for live UI, and the
	// full-queue case only happens once the DB has been unreachable long enough
	// to fill the channel (~25s, see dbLogChannelSize) and then hold this caller
	// for sendTimeout on top. Reported, because a history with holes in it and
	// no notice is worse than a slow caller.
	reason := err.Error()
	if errors.Is(err, errLogWriterQueueFull) {
		reason += " for " + w.sendTimeout.String()
	}
	w.drops.drop(1, reason)
}

// flushBarrier returns once everything queued before the call has been written
// to the database. ClearAppLogs holds the purge behind it so entries still in
// the queue cannot flush after the DELETE and reinstate rows that were just
// removed. The whole wait, enqueue included, is bounded by timeout.
func (w *dbLogWriter) flushBarrier(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	done := make(chan struct{})
	if err := w.send(logMsg{flushed: done}, timer.C); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errLogWriterFlushSlow
	}
}

// stop retires the queue and waits for the run goroutine to write out what is
// left, bounded end to end by stopDrainTimeout.
//
// The deadline is armed before the lock, because the senders already in flight
// hold the read lock for up to their own sendTimeout and a deadline armed after
// acquiring it would not bound this stage at all. Cancelling w.life ends all
// three things it has to cover at once: a sender parked on a full queue, a
// flush already under way, and the tail drain.
//
// Marking closed under the write lock is what makes it safe against a
// concurrent write: every sender holds the read lock across its send, so once
// this returns from Lock no send is in flight and none can start. Waiting out
// those senders is the price of the guarantee this exists for, that a send
// which returned nil is a row in the database. Senders arriving from here on
// are refused at once rather than queued behind this lock.
func (w *dbLogWriter) stop() {
	deadline := time.AfterFunc(stopDrainTimeout, w.endLife)
	defer deadline.Stop()

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		// A second caller waits for the first caller's drain too. Returning on
		// the flag alone would tell it the writer is stopped while batches are
		// still going into a pool it is about to close, which is the same
		// "stopped is not drained" reading the purge barrier refuses.
		<-w.done
		return
	}
	w.closed = true
	close(w.stopping)
	w.mu.Unlock()
	<-w.done
	w.endLife()
	w.drops.reportPending()
}

// InitAppLogBuffer initializes the application log ring buffer and optional DB writer.
func InitAppLogBuffer(pool *pgxpool.Pool) {
	appLogBuffer = &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}
	if pool != nil {
		dbWriter.Store(newDBLogWriter(pool, dbLogFlushInterval))
	}
	log.SetOutput(io.MultiWriter(&stderrLogFilter{dst: os.Stderr}, appLogBuffer))
}

// appLogFlushBarrierTimeout bounds the purge's wait for the async writer. Long
// enough for a healthy writer to drain a full batch, short enough that a stalled
// one does not hold the operator's request open.
const appLogFlushBarrierTimeout = 5 * time.Second

// flushAppLogWriter drains everything the async writer has queued so far, so a
// purge cannot be followed by a flush that reinstates the rows it deleted. No
// writer means nothing to drain.
func flushAppLogWriter(timeout time.Duration) error {
	if w := dbWriter.Load(); w != nil {
		return w.flushBarrier(timeout)
	}
	return nil
}

// StopAppLogWriter stops the database log writer goroutine, bounded by
// stopDrainTimeout: what the queue still holds is written out inside it and the
// rest is counted as dropped on stderr.
//
// The global keeps pointing at the stopped writer. Clearing it would make every
// producer skip the writer entirely, so the lines logged from here to process
// exit would vanish with no notice at all; leaving it set means each one is
// refused by the writer's own closed state and counted as a drop. It also keeps
// a purge arriving afterwards behind the barrier, which answers 503, rather than
// reading a nil as "no writer, nothing to flush" and deleting rows.
func StopAppLogWriter() {
	if w := dbWriter.Load(); w != nil {
		w.stop()
	}
}

// writeEntry adds a pre-built AppLogEntry to the ring buffer (no text parsing).
func (rb *ringBuffer) writeEntry(entry AppLogEntry) {
	rb.mu.Lock()
	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % appLogBufferSize
	if rb.count < appLogBufferSize {
		rb.count++
	}
	rb.mu.Unlock()
}

// Write implements io.Writer so ringBuffer can be used with log.SetOutput.
// It splits multi-line output into individual entries.
func (rb *ringBuffer) Write(p []byte) (n int, err error) {
	text := string(p)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	now := time.Now().UTC()
	for _, line := range lines {
		if line == "" {
			continue
		}
		source, level, msg := parseLogLine(line)
		entry := AppLogEntry{
			Timestamp: now.Format(time.RFC3339Nano),
			Level:     level,
			Source:    source,
			Message:   msg,
		}
		rb.writeEntry(entry)
		if w := dbWriter.Load(); w != nil {
			w.write(entry)
		}
	}
	return len(p), nil
}

// GetEntries returns all buffered entries in chronological order (oldest first).
func (rb *ringBuffer) GetEntries() []AppLogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	result := make([]AppLogEntry, rb.count)
	start := 0
	if rb.count == appLogBufferSize {
		start = rb.head // oldest entry is at head when buffer is full
	}
	for i := 0; i < rb.count; i++ {
		result[i] = rb.entries[(start+i)%appLogBufferSize]
	}
	return result
}

// Clear resets the ring buffer, returning the number of entries that were cleared.
func (rb *ringBuffer) Clear() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	n := rb.count
	rb.head = 0
	rb.count = 0
	return n
}

// ClearOlderThan drops buffered entries whose event timestamp predates cutoff,
// compacting the survivors back to the front of the buffer. Entries with an
// unparseable timestamp are kept (we never discard a log we can't date).
// Returns the number of entries removed.
func (rb *ringBuffer) ClearOlderThan(cutoff time.Time) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.count == 0 {
		return 0
	}

	// Walk oldest -> newest, mirroring GetEntries' start/index math.
	start := 0
	if rb.count == appLogBufferSize {
		start = rb.head
	}
	kept := make([]AppLogEntry, 0, rb.count)
	removed := 0
	for i := 0; i < rb.count; i++ {
		e := rb.entries[(start+i)%appLogBufferSize]
		if ts, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil && ts.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, e)
	}

	// Rebuild the backing array from the survivors, oldest at index 0.
	for i := range rb.entries {
		rb.entries[i] = AppLogEntry{}
	}
	copy(rb.entries, kept)
	rb.count = len(kept)
	rb.head = rb.count % appLogBufferSize
	return removed
}
