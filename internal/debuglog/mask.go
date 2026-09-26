package debuglog

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
)

// masker scrubs credentials out of log text on its way to every installed
// handler. It is the one pass every log line in a binary takes, so a provider
// key that reaches a log through any call site (a transport error quoting a
// URL whose query carries the key, a provider echoing the key back in an error
// frame, a hostname in a redirect) is masked without that call site having to
// remember to do it. Per-site masking was tried first and never converged:
// each review round found another site that had forgotten.
//
// debuglog cannot import util, which already imports debuglog, so each binary
// hands its masker in with SetMasker at startup. Until it does, records pass
// through unchanged.
//
// This masks credentials and nothing else. It does not escape, quote or
// reshape anything, so a log line stays exactly as readable as it was; the
// readers that must not be fooled by caller-controlled text defend themselves
// (see StdoutHandler).
var masker atomic.Pointer[func(string) string]

// SetMasker installs the function a record's message and every string-valued
// attribute pass through before any handler sees them. Safe to call at any
// time; a nil fn turns masking off.
func SetMasker(fn func(string) string) {
	if fn == nil {
		masker.Store(nil)
		return
	}
	masker.Store(&fn)
}

// withMasking wraps h in the masker, unless it is already wrapped: a caller
// restoring a saved default hands the masked handler straight back.
func withMasking(h slog.Handler) slog.Handler {
	if _, ok := h.(maskingHandler); ok {
		return h
	}
	return maskingHandler{h}
}

// maskingHandler applies the installed masker, then hands the record on. It
// sits inside the scope filter, so a Debug record that is going to be dropped
// is dropped before it pays for the mask.
type maskingHandler struct{ next slog.Handler }

func (h maskingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h maskingHandler) Handle(ctx context.Context, r slog.Record) error {
	fn := masker.Load()
	if fn == nil {
		return h.next.Handle(ctx, r)
	}
	out := slog.NewRecord(r.Time, r.Level, (*fn)(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(maskAttr(*fn, a))
		return true
	})
	return h.next.Handle(ctx, out)
}

// WithAttrs masks attributes as they are attached (logger.With), since they
// are rendered on every later record without passing through Handle's attrs.
// The mask is the one in force at attach time: a key held later is not
// scrubbed from attributes attached before it. No production logger is built
// with With, and keys are held at startup before any request is logged.
func (h maskingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if fn := masker.Load(); fn != nil {
		masked := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			masked[i] = maskAttr(*fn, a)
		}
		attrs = masked
	}
	return maskingHandler{h.next.WithAttrs(attrs)}
}

func (h maskingHandler) WithGroup(name string) slog.Handler {
	return maskingHandler{h.next.WithGroup(name)}
}

// maskAttr masks the text an attribute will render as (see maskAny). An
// attribute the mask leaves unchanged keeps its value as given.
func maskAttr(fn func(string) string, a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, fn(v.String()))
	case slog.KindGroup:
		group := v.Group()
		masked := make([]slog.Attr, len(group))
		for i, g := range group {
			masked[i] = maskAttr(fn, g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(masked...)}
	case slog.KindAny:
		if masked, ok := maskAny(fn, v.Any()); ok {
			return slog.Any(a.Key, masked)
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}

// maskDepth bounds the walk into nested collections: a slice can contain
// itself, and the walk must end whatever it is handed.
const maskDepth = 8

// depthMarker replaces a collection nested past maskDepth. Forwarding it
// untouched would let a credential ride a ninth level of nesting past the
// masker, and no real log line nests that deep.
const depthMarker = "[omitted: nested past the log masker's depth]"

// maskAny masks one KindAny value, reporting whether the mask changed anything
// a handler renders. Nothing that masks to itself is replaced, so a value with
// no credential in it keeps its own type and every sink renders it exactly as
// before. A nil or typed-nil value is left alone: calling Error or String on
// one panics, where slog itself renders "<nil>", and this handler sits in front
// of every record in the binary, so a (*T)(nil) error logged from a background
// goroutine must not become a crash.
//
// Collections are walked by kind, not by exact type, so a map[string]string, a
// []error, an array and a named slice or map type are masked element by
// element (map keys included) and keep their structure. Everything else that
// carries text (a struct, a pointer, an error, a Stringer, a type with its own
// MarshalJSON or MarshalText) is rendered the way each sink renders it (see
// maskRendered) and, when any rendering masks differently, replaced by the
// masked text. That replacement is a string in every sink, so a value with a
// MarshalJSON that masks differently reaches the JSON sink as a JSON string,
// not the object it marshals to.
func maskAny(fn func(string) string, x any) (any, bool) {
	return maskAnyDepth(fn, x, maskDepth)
}

func maskAnyDepth(fn func(string) string, x any, depth int) (any, bool) {
	if x == nil || isTypedNil(x) {
		return nil, false
	}
	switch v := x.(type) {
	case string:
		m := fn(v)
		return m, m != v
	case []byte:
		// Masked as the text it holds, and kept a []byte so each handler
		// renders it exactly as before.
		if m := fn(string(v)); m != string(v) {
			return []byte(m), true
		}
		return nil, false
	case json.Marshaler, encoding.TextMarshaler, error, fmt.Stringer:
		// The type decides its own rendering, so its elements are not walked.
		return maskRendered(fn, x)
	}
	rv := reflect.ValueOf(x)
	switch rv.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return nil, false
	case reflect.String:
		// A named string type: the switch above matched only string itself.
		s := rv.String()
		m := fn(s)
		return m, m != s
	}
	if depth == 0 {
		return depthMarker, true
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return maskSlice(fn, rv, depth-1)
	case reflect.Map:
		return maskMap(fn, rv, depth-1)
	}
	return maskRendered(fn, x)
}

// maskSlice masks each element, rebuilding the slice only when one changed.
func maskSlice(fn func(string) string, rv reflect.Value, depth int) (any, bool) {
	out := make([]any, rv.Len())
	changed := false
	for i := range out {
		e := rv.Index(i).Interface()
		if m, ok := maskAnyDepth(fn, e, depth); ok {
			out[i], changed = m, true
			continue
		}
		out[i] = e
	}
	if !changed {
		return nil, false
	}
	return out, true
}

// maskMap masks each key and value, rebuilding the map only when one changed.
// Keys become field names, rendered to strings for a map with non-string
// keys. Two keys that render or mask to the same name (two credentials both
// masked to "[redacted]") keep both entries: the later one gets a "#2", "#3"
// suffix rather than overwriting the first. Entries are taken in the order of
// their rendered keys, not map order, so the same map gets the same suffixes
// on every record.
func maskMap(fn func(string) string, rv reflect.Value, depth int) (any, bool) {
	type entry struct {
		key string
		val any
	}
	entries := make([]entry, 0, rv.Len())
	for iter := rv.MapRange(); iter.Next(); {
		entries = append(entries, entry{fmt.Sprint(iter.Key().Interface()), iter.Value().Interface()})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	out := make(map[string]any, len(entries))
	changed := false
	for _, e := range entries {
		mk := fn(e.key)
		if mk != e.key {
			changed = true
		}
		if _, taken := out[mk]; taken {
			changed = true
			for n := 2; ; n++ {
				cand := fmt.Sprintf("%s#%d", mk, n)
				if _, taken := out[cand]; !taken {
					mk = cand
					break
				}
			}
		}
		v := e.val
		if m, ok := maskAnyDepth(fn, v, depth); ok {
			v, changed = m, true
		}
		out[mk] = v
	}
	if !changed {
		return nil, false
	}
	return out, true
}

// maskRendered masks a value through every text a sink renders it as, and
// replaces it with the first masked rendering that differs. Checking one
// rendering is not enough: the text handler prints a struct's nested pointer
// as an address where the JSON sink prints the string behind it, and a type's
// own MarshalJSON or MarshalText can emit text neither %+v nor String shows.
// The text rendering is masked only when it differs from the JSON one, which
// for an error or a Stringer it almost never does.
func maskRendered(fn func(string) string, x any) (any, bool) {
	js, jsOK := jsonRendering(x)
	if jsOK {
		if m := fn(js); m != js {
			return m, true
		}
	}
	if ts, ok := textRendering(x); ok && (!jsOK || ts != js) {
		if m := fn(ts); m != ts {
			return m, true
		}
	}
	return nil, false
}

// jsonRendering is the text the JSON sink and the app-log fields render x as
// (jsonValue), reporting false when rendering it fails or panics. The
// encoder ends a pointer cycle itself, with an error.
func jsonRendering(x any) (s string, ok bool) {
	defer func() {
		if recover() != nil {
			s, ok = "", false
		}
	}()
	v := jsonValue(slog.AnyValue(x))
	if raw, isRaw := v.(json.RawMessage); isRaw {
		return string(raw), true
	}
	return fmt.Sprint(v), true
}

// textRendering is the text slog's text handler renders a KindAny value as:
// MarshalText for a TextMarshaler, %+v for anything else. fmt recovers a
// panicking String, Error or Format method itself; MarshalText is guarded.
func textRendering(x any) (s string, ok bool) {
	defer func() {
		if recover() != nil {
			s, ok = "", false
		}
	}()
	if tm, isTM := x.(encoding.TextMarshaler); isTM {
		b, err := tm.MarshalText()
		return string(b), err == nil
	}
	return fmt.Sprintf("%+v", x), true
}
