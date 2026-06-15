package debuglog

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// recHandler is a test slog.Handler that records the calls it receives.
type recHandler struct {
	enabled    bool
	handleErr  error
	handled    int
	lastAttrs  []slog.Attr
	lastGroup  string
	lastRecord slog.Record
}

func (h *recHandler) Enabled(context.Context, slog.Level) bool { return h.enabled }
func (h *recHandler) Handle(_ context.Context, r slog.Record) error {
	h.handled++
	h.lastRecord = r
	return h.handleErr
}
func (h *recHandler) WithAttrs(a []slog.Attr) slog.Handler { h.lastAttrs = a; return h }
func (h *recHandler) WithGroup(n string) slog.Handler      { h.lastGroup = n; return h }

func TestNewFanout_CollapsesTrivialCases(t *testing.T) {
	if got := NewFanout(); got != slog.DiscardHandler {
		t.Errorf("NewFanout() with no handlers = %T, want DiscardHandler", got)
	}

	h1 := &recHandler{enabled: true}
	if got := NewFanout(nil, h1, nil); got != h1 {
		t.Errorf("NewFanout(nil, h1, nil) = %T, want the single handler unwrapped", got)
	}

	if _, ok := NewFanout(&recHandler{}, &recHandler{}).(fanoutHandler); !ok {
		t.Errorf("NewFanout with two handlers should return a fanoutHandler")
	}
}

func TestFanout_HandleDispatchesToEnabledChildrenOnly(t *testing.T) {
	on := &recHandler{enabled: true}
	off := &recHandler{enabled: false}
	on2 := &recHandler{enabled: true}

	fan := NewFanout(on, off, on2)

	if !fan.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled should be true when any child is enabled")
	}

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	if err := fan.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if on.handled != 1 || on2.handled != 1 {
		t.Errorf("enabled children handled = %d/%d, want 1/1", on.handled, on2.handled)
	}
	if off.handled != 0 {
		t.Errorf("disabled child handled = %d, want 0", off.handled)
	}
}

func TestFanout_EnabledFalseWhenAllChildrenDisabled(t *testing.T) {
	fan := NewFanout(&recHandler{enabled: false}, &recHandler{enabled: false})
	if fan.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled should be false when no child is enabled")
	}
}

func TestFanout_HandleJoinsChildErrors(t *testing.T) {
	boom := errors.New("boom")
	bad := &recHandler{enabled: true, handleErr: boom}
	good := &recHandler{enabled: true}

	fan := NewFanout(bad, good)
	err := fan.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0))
	if !errors.Is(err, boom) {
		t.Errorf("Handle error = %v, want it to wrap boom", err)
	}
	if good.handled != 1 {
		t.Errorf("a failing child must not stop dispatch to others; good.handled = %d", good.handled)
	}
}

func TestFanout_WithAttrsAndGroupPropagate(t *testing.T) {
	a := &recHandler{enabled: true}
	b := &recHandler{enabled: true}

	fan := NewFanout(a, b)
	attrs := []slog.Attr{slog.String("k", "v")}
	_ = fan.WithAttrs(attrs).WithGroup("grp").Handle(
		context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "m", 0),
	)

	for name, h := range map[string]*recHandler{"a": a, "b": b} {
		if len(h.lastAttrs) != 1 || h.lastAttrs[0].Key != "k" {
			t.Errorf("child %s did not receive WithAttrs", name)
		}
		if h.lastGroup != "grp" {
			t.Errorf("child %s did not receive WithGroup, got %q", name, h.lastGroup)
		}
	}
}
