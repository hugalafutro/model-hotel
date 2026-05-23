package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
)

// NewAppSlogHandler returns a slog.Handler that writes structured log entries
// through the app log pipeline (ring buffer + DB writer + filtered stderr).
// Call after InitAppLogBuffer and pass to debuglog.SetHandler to route all
// slog output through the app logging system.
func NewAppSlogHandler(level slog.Level) slog.Handler {
	return &appSlogHandler{
		level:  level,
		stderr: &stderrLogFilter{dst: os.Stderr},
	}
}

// appSlogHandler implements slog.Handler by creating AppLogEntry values
// directly from slog records, routing them through the ring buffer and DB
// writer, and conditionally forwarding to stderr for docker logs.
type appSlogHandler struct {
	level  slog.Level
	stderr *stderrLogFilter
	group  string
	attrs  []slog.Attr
}

func (h *appSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *appSlogHandler) Handle(_ context.Context, r slog.Record) error {
	// Build message: prepend group prefix if set, then append any attrs.
	var msg strings.Builder
	if h.group != "" {
		fmt.Fprintf(&msg, "[%s] ", h.group)
	}
	msg.WriteString(r.Message)

	// Append any handler-level attrs.
	for _, a := range h.attrs {
		fmt.Fprintf(&msg, " %s=%v", a.Key, a.Value)
	}
	// Append per-record attrs.
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&msg, " %s=%v", a.Key, a.Value)
		return true
	})

	// Map slog level to app level.
	appLevel := "info"
	switch {
	case r.Level >= slog.LevelError:
		appLevel = "error"
	case r.Level >= slog.LevelWarn:
		appLevel = "warning"
	case r.Level >= slog.LevelInfo:
		appLevel = "info"
	case r.Level >= slog.LevelDebug:
		appLevel = "debug"
	}

	// Extract source from "[source]" prefix in message, same as parseLogLine.
	source, msgStr := extractSource(msg.String())
	// For slog entries, the level is authoritative — do not let the text
	// heuristic (detectLevel) override it.  Field values like "error_chunks=0"
	// or "has_error=false" would falsely trigger detectLevel's "error" match.
	// The heuristic remains useful for legacy log.Printf lines (Write path).
	msgStr = stripLevelPrefix(msgStr)

	entry := AppLogEntry{
		Timestamp: r.Time.UTC().Format(time.RFC3339Nano),
		Level:     appLevel,
		Source:    source,
		Message:   msgStr,
	}

	// Write to ring buffer and DB.
	if appLogBuffer != nil {
		appLogBuffer.writeEntry(entry)
	}
	if w := dbWriter; w != nil {
		w.write(entry)
	}

	// Forward to stderr filter for docker logs.
	_, _ = fmt.Fprintf(h.stderr, "%s level=%s %s\n",
		r.Time.Format("2006/01/02 15:04:05"),
		strings.ToUpper(appLevel),
		msg.String())

	return nil
}

func (h *appSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &appSlogHandler{
		level:  h.level,
		stderr: h.stderr,
		group:  h.group,
		attrs:  append(slices.Clone(h.attrs), attrs...),
	}
}

func (h *appSlogHandler) WithGroup(name string) slog.Handler {
	if h.group != "" {
		name = h.group + "." + name
	}
	return &appSlogHandler{
		level:  h.level,
		stderr: h.stderr,
		group:  name,
		attrs:  slices.Clone(h.attrs),
	}
}
