package debuglog

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
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

// maskAttr masks the text an attribute will render as. Strings, errors and
// Stringers are masked as the string both the text and the JSON handler render
// them to; the slices and maps a call site assembles are walked, so a
// []string of error texts or a metadata map is masked element by element.
// Anything else keeps its structure: there is no text in it to scrub.
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

// maskAny masks one KindAny value, reporting whether it changed anything the
// handler renders. A nil or typed-nil value is left alone: calling Error or
// String on one panics, where slog itself renders "<nil>", and this handler
// sits in front of every record in the binary, so a (*T)(nil) error logged
// from a background goroutine must not become a crash.
//
// Collections are walked by kind, not by exact type, so a map[string]string, a
// []error, an array and a named slice or map type are masked as well as the
// []any a call site might assemble. Anything else that carries text (a struct,
// a named string type) is rendered as the handlers would render it and masked;
// it keeps its own value unless the mask actually changed something, so a
// struct without a credential in it is logged exactly as before.
func maskAny(fn func(string) string, x any) (any, bool) {
	return maskAnyDepth(fn, x, maskDepth)
}

func maskAnyDepth(fn func(string) string, x any, depth int) (any, bool) {
	if x == nil || isTypedNil(x) {
		return nil, false
	}
	switch v := x.(type) {
	case string:
		return fn(v), true
	case []byte:
		// Masked as the text it holds, and kept a []byte so each handler
		// renders it exactly as before.
		return []byte(fn(string(v))), true
	case error:
		if s, ok := safeText(v.Error); ok {
			return fn(s), true
		}
		return nil, false
	case fmt.Stringer:
		if s, ok := safeText(v.String); ok {
			return fn(s), true
		}
		return nil, false
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
		return fn(rv.String()), true
	}
	if depth == 0 {
		return depthMarker, true
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = maskElem(fn, rv.Index(i).Interface(), depth-1)
		}
		return out, true
	case reflect.Map:
		// Keys are kept as field names, rendered to strings for a map with
		// non-string keys; they are not masked, since two keys masked to the
		// same "[redacted]" would silently drop an entry.
		out := make(map[string]any, rv.Len())
		for iter := rv.MapRange(); iter.Next(); {
			out[fmt.Sprint(iter.Key().Interface())] = maskElem(fn, iter.Value().Interface(), depth-1)
		}
		return out, true
	}
	// A struct, a pointer to one, an interface: rendered the way the text
	// handler prints it, and replaced by the masked rendering only if that
	// differs. fmt recovers a panicking String, Error or Format method itself
	// (it prints "%!v(PANIC=...)"), so no guard is needed here.
	rendered := fmt.Sprintf("%+v", x)
	if masked := fn(rendered); masked != rendered {
		return masked, true
	}
	return nil, false
}

// maskElem masks one element of a collection, leaving it unchanged when it
// carries no text.
func maskElem(fn func(string) string, e any, depth int) any {
	if masked, ok := maskAnyDepth(fn, e, depth); ok {
		return masked
	}
	return e
}

// safeText calls an Error or String method, reporting false when it panics.
// isTypedNil catches the common case, but an error whose Error dereferences a
// nil field inside a non-nil value (a *url.Error with a nil Err) panics too,
// and slog recovers exactly that for the handlers behind this one.
func safeText(render func() string) (s string, ok bool) {
	defer func() {
		if recover() != nil {
			s, ok = "", false
		}
	}()
	return render(), true
}
