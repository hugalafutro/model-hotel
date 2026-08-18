package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

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

// quoteLogValue renders an attribute value for the flattened k=v text form.
// A value holding a space, an '=', a quote, a backslash or a control character
// is quoted, and the spaces inside it are escaped as well, so the result is a
// single whitespace-delimited token no matter what the caller put in it.
//
// Escaping the spaces is the part that matters. Request paths and virtual key
// names are caller-controlled and are logged as attributes, so a value like
//
//	pwn remote_addr=203.0.113.99 x
//
// would otherwise still contain a "key=value" token after quoting, and any
// reader that splits on whitespace before it considers quotes (a CrowdSec
// grok, a fail2ban regex, an awk one-liner) would read that token as the
// gateway's own. strconv.Unquote reverses \x20, so the value round-trips.
//
// Values with nothing to escape stay bare, so ordinary lines read as before.
func quoteLogValue(v any) string {
	s := fmt.Sprintf("%v", v)
	if s == "" || strings.ContainsAny(s, " =\"\\") || strings.ContainsFunc(s, unicode.IsControl) {
		return strings.ReplaceAll(strconv.Quote(s), " ", `\x20`)
	}
	return s
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

	fields := make(map[string]any)
	appendAttr := func(a slog.Attr) {
		fmt.Fprintf(&msg, " %s=%s", a.Key, quoteLogValue(a.Value))
		// Same value rules as every other JSON emitter (debuglog.AddJSONField):
		// typed where JSON has a type, textual otherwise, nothing dropped.
		debuglog.AddJSONField(fields, "", a)
	}
	// Handler-level attrs first, then per-record attrs.
	for _, a := range h.attrs {
		appendAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(a)
		return true
	})

	appLevel := debuglog.LevelName(r.Level)

	// Extract source from "[source]" prefix in message, same as parseLogLine.
	source, msgStr := debuglog.SplitSource(msg.String())
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
		_, jsonMsg := debuglog.SplitSource(baseMsg)
		jsonMsg = stripLevelPrefix(jsonMsg)
		_, _ = fmt.Fprintf(h.stderr, "%s\n", debuglog.JSONLine(r.Time, appLevel, source, jsonMsg, fields))
	} else {
		_, _ = fmt.Fprintf(h.stderr, "%s level=%s %s\n",
			r.Time.Format("2006/01/02 15:04:05"),
			strings.ToUpper(appLevel),
			msg.String())
	}

	return nil
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
