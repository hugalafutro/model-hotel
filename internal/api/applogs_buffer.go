package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
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

// appLogBuffer is the global ring buffer that captures log output.
var appLogBuffer *ringBuffer

// dbWriter is the asynchronous database log writer (nil if no pool).
var dbWriter *dbLogWriter

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

type dbLogWriter struct {
	pool *pgxpool.Pool
	ch   chan AppLogEntry
	done chan struct{}
}

func newDBLogWriter(pool *pgxpool.Pool) *dbLogWriter {
	w := &dbLogWriter{
		pool: pool,
		ch:   make(chan AppLogEntry, dbLogChannelSize),
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *dbLogWriter) run() {
	batch := make([]AppLogEntry, 0, 50)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-w.ch:
			if !ok {
				// Channel closed — flush remaining
				if len(batch) > 0 {
					w.flush(batch)
				}
				close(w.done)
				return
			}
			batch = append(batch, entry)
			if len(batch) >= 50 {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *dbLogWriter) flush(entries []AppLogEntry) {
	if w.pool == nil || len(entries) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Build batch INSERT
	builder := strings.Builder{}
	builder.WriteString("INSERT INTO app_logs (timestamp, level, source, message) VALUES ")
	args := make([]interface{}, 0, len(entries)*4)
	for i, e := range entries {
		if i > 0 {
			builder.WriteString(", ")
		}
		offset := i * 4
		fmt.Fprintf(&builder, "($%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4)
		args = append(args, e.Timestamp, e.Level, e.Source, e.Message)
	}
	_, err := w.pool.Exec(ctx, builder.String(), args...)
	if err != nil {
		// Don't log with log.Printf here — that would cause infinite recursion!
		// Just silently drop — the ring buffer still has the data for live view.
		_ = err
	}
}

func (w *dbLogWriter) write(entry AppLogEntry) {
	defer func() { recover() }()
	timer := time.NewTimer(dbLogSendTimeout)
	defer timer.Stop()
	select {
	case w.ch <- entry:
		return
	case <-timer.C:
		// DB writer is backed up — drop the entry rather than blocking the
		// caller. The ring buffer still has it for live UI, and this only
		// happens if the DB is unreachable for >25 seconds.
	}
}

func (w *dbLogWriter) stop() {
	close(w.ch)
	<-w.done
}

// InitAppLogBuffer initializes the application log ring buffer and optional DB writer.
func InitAppLogBuffer(pool *pgxpool.Pool) {
	appLogBuffer = &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}
	if pool != nil {
		dbWriter = newDBLogWriter(pool)
	}
	log.SetOutput(io.MultiWriter(&stderrLogFilter{dst: os.Stderr}, appLogBuffer))
}

// StopAppLogWriter stops the database log writer goroutine.
func StopAppLogWriter() {
	if dbWriter != nil {
		w := dbWriter
		dbWriter = nil
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
		rb.mu.Lock()
		rb.entries[rb.head] = entry
		rb.head = (rb.head + 1) % appLogBufferSize
		if rb.count < appLogBufferSize {
			rb.count++
		}
		rb.mu.Unlock()
		if w := dbWriter; w != nil {
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
