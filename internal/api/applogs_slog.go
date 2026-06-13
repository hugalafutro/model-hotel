package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// NewAppSlogHandler returns a slog.Handler that writes structured log entries
// through the app log pipeline (ring buffer + DB writer + filtered stderr).
// Call after InitAppLogBuffer and pass to debuglog.SetHandler to route all
// slog output through the app logging system.
//
// The docker-logs (stderr) surface honors LOG_FORMAT: when JSON is requested it
// emits one JSON object per line (level/source/msg + the slog attrs as fields)
// for log collectors; otherwise the human-readable text form. The ring buffer /
// DB / SSE path (the App Logs page) is unchanged either way.
func NewAppSlogHandler(level slog.Level) slog.Handler {
	return &appSlogHandler{
		level:      level,
		stderr:     &stderrLogFilter{dst: os.Stderr},
		jsonOutput: debuglog.JSONFormat(),
	}
}

// appSlogHandler implements slog.Handler by creating AppLogEntry values
// directly from slog records, routing them through the ring buffer and DB
// writer, and conditionally forwarding to stderr for docker logs.
type appSlogHandler struct {
	level      slog.Level
	stderr     *stderrLogFilter
	group      string
	attrs      []slog.Attr
	jsonOutput bool // emit JSON (not k=v text) to stderr for log collectors
}

func (h *appSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *appSlogHandler) Handle(_ context.Context, r slog.Record) error {
	// Build message: prepend group prefix if set, then append any attrs.
	// Collect the attrs as discrete key/value pairs too, so the JSON stderr
	// path can emit them as real fields instead of the flattened k=v text.
	var msg strings.Builder
	if h.group != "" {
		fmt.Fprintf(&msg, "[%s] ", h.group)
	}
	msg.WriteString(r.Message)
	baseMsg := msg.String() // message before attrs are appended (for the JSON field)

	fields := make(map[string]string)
	appendAttr := func(a slog.Attr) {
		fmt.Fprintf(&msg, " %s=%v", a.Key, a.Value)
		// String() gives a stable textual form for every slog.Kind (incl.
		// errors/durations), which is what collectors index; no value is
		// dropped the way json.Marshal would drop a bare error.
		fields[a.Key] = a.Value.String()
	}
	// Handler-level attrs first, then per-record attrs.
	for _, a := range h.attrs {
		appendAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(a)
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

	// Forward to the stderr filter for docker logs, in the configured format.
	if h.jsonOutput {
		// Use the attr-free message for the JSON "msg" field; attrs are emitted
		// as their own fields (the ring-buffer Message still flattens them).
		_, jsonMsg := extractSource(baseMsg)
		jsonMsg = stripLevelPrefix(jsonMsg)
		_, _ = fmt.Fprintf(h.stderr, "%s\n", h.renderJSONLine(r.Time, appLevel, source, jsonMsg, fields))
	} else {
		_, _ = fmt.Fprintf(h.stderr, "%s level=%s %s\n",
			r.Time.Format("2006/01/02 15:04:05"),
			strings.ToUpper(appLevel),
			msg.String())
	}

	return nil
}

// renderJSONLine builds one structured log line: the reserved time/level/source/msg
// keys plus each slog attr as its own field. Reserved keys win over a colliding
// attr key so the line's shape is always predictable for collectors.
func (h *appSlogHandler) renderJSONLine(t time.Time, level, source, msg string, fields map[string]string) []byte {
	obj := make(map[string]string, len(fields)+4)
	for k, v := range fields {
		obj[k] = v
	}
	obj["time"] = t.UTC().Format(time.RFC3339Nano)
	obj["level"] = level
	obj["source"] = source
	obj["msg"] = msg
	b, err := json.Marshal(obj)
	if err != nil {
		// Marshaling a map[string]string cannot realistically fail; fall back
		// to a minimal valid object rather than dropping the line.
		return []byte(fmt.Sprintf(`{"level":%q,"msg":%q}`, level, msg))
	}
	return b
}

func (h *appSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &appSlogHandler{
		level:      h.level,
		stderr:     h.stderr,
		group:      h.group,
		attrs:      append(slices.Clone(h.attrs), attrs...),
		jsonOutput: h.jsonOutput,
	}
}

func (h *appSlogHandler) WithGroup(name string) slog.Handler {
	if h.group != "" {
		name = h.group + "." + name
	}
	return &appSlogHandler{
		level:      h.level,
		stderr:     h.stderr,
		group:      name,
		attrs:      slices.Clone(h.attrs),
		jsonOutput: h.jsonOutput,
	}
}
