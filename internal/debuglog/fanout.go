package debuglog

import (
	"context"
	"errors"
	"log/slog"
)

// fanoutHandler dispatches each log record to every child handler, so a single
// debuglog call can reach multiple destinations at once — e.g. the existing
// app-log pipeline (stderr + ring buffer + database) AND an OTLP exporter.
// WithAttrs/WithGroup propagate to every child so derived loggers stay in sync.
type fanoutHandler struct {
	handlers []slog.Handler
}

// NewFanout returns a slog.Handler that forwards records to all of the given
// handlers. Nil handlers are skipped. As a convenience it collapses the trivial
// cases: zero non-nil handlers yields a no-op handler, exactly one yields that
// handler unwrapped (no fan-out overhead).
func NewFanout(handlers ...slog.Handler) slog.Handler {
	nonNil := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			nonNil = append(nonNil, h)
		}
	}
	switch len(nonNil) {
	case 0:
		return slog.DiscardHandler
	case 1:
		return nonNil[0]
	default:
		return fanoutHandler{handlers: nonNil}
	}
}

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, c := range h.handlers {
		if c.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, c := range h.handlers {
		if !c.Enabled(ctx, r.Level) {
			continue
		}
		// A child may retain the record beyond the call (the OTLP batch
		// processor buffers records before export), so hand each its own clone.
		if err := c.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, c := range h.handlers {
		next[i] = c.WithAttrs(attrs)
	}
	return fanoutHandler{handlers: next}
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := make([]slog.Handler, len(h.handlers))
	for i, c := range h.handlers {
		next[i] = c.WithGroup(name)
	}
	return fanoutHandler{handlers: next}
}
