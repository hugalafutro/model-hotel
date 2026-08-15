package debuglog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"
	"unicode"
)

// LevelName maps a slog level to the lowercase level names Model Hotel uses
// everywhere a level is rendered for machines (the App Logs API, LOG_FORMAT=json
// lines): debug, info, warning, error. Kept in one place so every JSON emitter
// agrees; a collector that labels by level must never see both "WARN" and
// "warning" from the same process.
func LevelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warning"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// SplitSource separates the source prefix from a log message. Two forms are
// recognised:
//   - Bracketed: "[proxy] message" → ("proxy", "message")
//   - Colon-separated: "proxy: message" → ("proxy", "message"), where the
//     source is at least 2 chars and matches [a-zA-Z_][a-zA-Z0-9._-]*
//
// With no source prefix it returns ("", msg).
func SplitSource(msg string) (string, string) {
	if msg != "" && msg[0] == '[' {
		end := strings.Index(msg, "]")
		if end > 0 && end < len(msg)-1 && msg[end+1] == ' ' {
			return msg[1:end], msg[end+2:]
		}
	}
	if colon := strings.Index(msg, ": "); colon >= 2 {
		candidate := msg[:colon]
		valid := true
		for i, ch := range candidate {
			if i == 0 {
				if !unicode.IsLetter(ch) && ch != '_' {
					valid = false
					break
				}
			} else if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '.' && ch != '-' {
				valid = false
				break
			}
		}
		if valid {
			return candidate, msg[colon+2:]
		}
	}
	return "", msg
}

// JSONLine renders one LOG_FORMAT=json record: the reserved time/level/source/
// msg keys plus each attr as its own field. Reserved keys win over a colliding
// attr key so the line's shape is always predictable for collectors. This is
// the single definition of the JSON line shape; every stdout/stderr JSON
// emitter (this package's handler, the dashboard's app-log handler) renders
// through it so the two never drift apart.
func JSONLine(t time.Time, level, source, msg string, fields map[string]string) []byte {
	obj := make(map[string]string, len(fields)+4)
	maps.Copy(obj, fields)
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

// jsonHandler is the LOG_FORMAT=json slog handler for the plain stdout sink
// (used before the dashboard installs its app-log handler, and by Front Desk
// throughout). It renders through JSONLine so its lines are byte-for-byte the
// same shape as the app-log handler's: slog's own JSONHandler would emit
// upper-case levels and leave the "scope: " prefix inside msg, giving a
// collector two record shapes from one process.
type jsonHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	level slog.Level
	attrs []slog.Attr
	group string
}

func newJSONHandler(w io.Writer, level slog.Level) *jsonHandler {
	return &jsonHandler{mu: &sync.Mutex{}, w: w, level: level}
}

func (h *jsonHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *jsonHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make(map[string]string, len(h.attrs)+r.NumAttrs())
	// Handler-level attrs were qualified with the group in force when they
	// were added (see WithAttrs); the record's own attrs get the current one.
	// String() gives a stable textual form for every slog.Kind (incl.
	// errors/durations), which is what collectors index.
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[h.qualify(a.Key)] = a.Value.String()
		return true
	})
	source, msg := SplitSource(r.Message)
	line := append(JSONLine(r.Time, LevelName(r.Level), source, msg, fields), '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(line)
	return err
}

// qualify prefixes key with the open group path, if any.
func (h *jsonHandler) qualify(key string) string {
	if h.group == "" {
		return key
	}
	return h.group + "." + key
}

func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := *h
	next.attrs = make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	next.attrs = append(next.attrs, h.attrs...)
	for _, a := range attrs {
		next.attrs = append(next.attrs, slog.Attr{Key: h.qualify(a.Key), Value: a.Value})
	}
	return &next
}

func (h *jsonHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	if h.group != "" {
		next.group = h.group + "." + name
	} else {
		next.group = name
	}
	return &next
}
