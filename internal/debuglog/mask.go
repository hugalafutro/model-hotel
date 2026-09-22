package debuglog

import (
	"context"
	"fmt"
	"log/slog"
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

// maskAttr masks the text an attribute will render as. An error or a Stringer
// is rendered through Error or String by both the text and the JSON handler,
// so it is masked as that string; any other non-string value keeps its
// structure, since there is no text in it to scrub.
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
		switch x := v.Any().(type) {
		case error:
			return slog.String(a.Key, fn(x.Error()))
		case fmt.Stringer:
			return slog.String(a.Key, fn(x.String()))
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
