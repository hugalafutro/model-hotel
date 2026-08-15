package debuglog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"reflect"
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
// msg keys plus each attr as its own field (as collected by AddJSONField).
// Reserved keys win over a colliding attr key so the line's shape is always
// predictable for collectors; a zero time is omitted per the slog contract.
// This is the single definition of the JSON line shape; every stdout/stderr
// JSON emitter (this package's handler, the dashboard's app-log handler)
// renders through it so the two never drift apart.
func JSONLine(t time.Time, level, source, msg string, fields map[string]any) []byte {
	obj := maps.Clone(fields)
	if obj == nil {
		obj = make(map[string]any, 4)
	}
	if !t.IsZero() {
		obj["time"] = t.UTC().Format(time.RFC3339Nano)
	}
	obj["level"] = level
	obj["source"] = source
	obj["msg"] = msg
	b, err := json.Marshal(obj)
	if err != nil {
		// Every value AddJSONField stores is marshalable, so this cannot
		// realistically fail; fall back to a minimal valid object rather than
		// dropping the line.
		return []byte(fmt.Sprintf(`{"level":%q,"msg":%q}`, level, msg))
	}
	return b
}

// AddJSONField stores attr a in fields in LOG_FORMAT=json value form, keyed
// by prefix.key (prefix is the dotted path of the slog groups in force, "" for
// none). It follows the slog.Handler contract: LogValuers are resolved, the
// zero Attr is ignored, groups expand into dotted keys (an unnamed group is
// inlined, an empty one dropped). Values keep their JSON type where one
// exists (numbers, bools, strings; JSON-marshalable values as-is) so a
// collector can index latency_ms as a number; durations, times and errors
// render as strings, and anything that cannot marshal falls back to its
// textual form so no value is ever dropped.
func AddJSONField(fields map[string]any, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		attrs := v.Group()
		if len(attrs) == 0 {
			return
		}
		p := prefix
		if a.Key != "" {
			p = joinKey(prefix, a.Key)
		}
		for _, ga := range attrs {
			AddJSONField(fields, p, ga)
		}
		return
	}
	if a.Key == "" {
		// The zero Attr per the slog contract; a keyless value has nowhere
		// sensible to go either.
		return
	}
	fields[joinKey(prefix, a.Key)] = jsonValue(v)
}

// joinKey builds the dotted key for an attr under a group prefix.
func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// jsonValue converts a resolved, non-group slog.Value to what JSONLine
// marshals for it (see AddJSONField for the rules).
func jsonValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		f := v.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return v.String() // JSON has no NaN/Inf; the text form keeps the line intact
		}
		return f
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	default:
		x := v.Any()
		if x == nil || isTypedNil(x) {
			return nil
		}
		// A type's own JSON form wins; otherwise its String()/Error() form
		// beats reflection: a Stringer that masks fields (config.Config) must
		// keep masking here, and reflection would dump the raw struct.
		if m, ok := x.(json.Marshaler); ok {
			if b, err := m.MarshalJSON(); err == nil {
				return json.RawMessage(b)
			}
		}
		if err, ok := x.(error); ok {
			return err.Error()
		}
		if str, ok := x.(fmt.Stringer); ok {
			return str.String()
		}
		if b, err := json.Marshal(x); err == nil {
			return json.RawMessage(b)
		}
		return v.String()
	}
}

// isTypedNil reports whether x is a nil pointer/map/slice/etc. boxed in an
// interface, so calling Error()/String() on it would dereference nil.
func isTypedNil(x any) bool {
	rv := reflect.ValueOf(x)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
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
	attrs []boundAttr // from WithAttrs, each with the group path open when it was added
	group string      // dotted path of the groups opened by WithGroup
}

// boundAttr is a handler-level attr together with the group prefix that was in
// force when WithAttrs added it (slog: attrs are qualified by the groups open
// at the time they are added, not at emit time).
type boundAttr struct {
	prefix string
	attr   slog.Attr
}

func newJSONHandler(w io.Writer, level slog.Level) *jsonHandler {
	return &jsonHandler{mu: &sync.Mutex{}, w: w, level: level}
}

func (h *jsonHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *jsonHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make(map[string]any, len(h.attrs)+r.NumAttrs())
	// Handler-level attrs are resolved per record (not once in WithAttrs), so
	// a LogValuer's value is the one current at emit time.
	for _, b := range h.attrs {
		AddJSONField(fields, b.prefix, b.attr)
	}
	r.Attrs(func(a slog.Attr) bool {
		AddJSONField(fields, h.group, a)
		return true
	})
	source, msg := SplitSource(r.Message)
	line := append(JSONLine(r.Time, LevelName(r.Level), source, msg, fields), '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(line)
	return err
}

func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := *h
	next.attrs = make([]boundAttr, 0, len(h.attrs)+len(attrs))
	next.attrs = append(next.attrs, h.attrs...)
	for _, a := range attrs {
		next.attrs = append(next.attrs, boundAttr{prefix: h.group, attr: a})
	}
	return &next
}

func (h *jsonHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.group = joinKey(h.group, name)
	return &next
}
