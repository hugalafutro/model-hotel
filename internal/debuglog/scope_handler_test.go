package debuglog

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestScopeFilterHandler_DerivedHandlersKeepFilter guards the WithAttrs/WithGroup
// paths of scopeFilterHandler: a logger derived with extra attrs or a group must
// still drop Debug records whose scope is not enabled. If either method returned
// the bare inner handler, scope filtering would silently leak on derived loggers.
func TestScopeFilterHandler_DerivedHandlersKeepFilter(t *testing.T) {
	// Register the global-state reset BEFORE t.Setenv so it runs AFTER t.Setenv's
	// own env restore (t.Cleanup is LIFO): Init then recomputes enabledScopes from
	// the restored DEBUG_LOG_SCOPES rather than from the unset value. t.Setenv
	// already restores/unsets the variable itself, so no manual os.Unsetenv.
	t.Cleanup(func() { Init(false) })
	t.Setenv("DEBUG_LOG_SCOPES", "failover")
	Init(false) // global debug off, scopes = {failover} -> wrapping is active

	capH := newCaptureHandler(slog.LevelDebug)
	wrapped := maybeScopeFilter(capH)
	if _, ok := wrapped.(scopeFilterHandler); !ok {
		t.Fatalf("maybeScopeFilter returned %T, want scopeFilterHandler", wrapped)
	}

	// Derive through both WithAttrs and WithGroup; the scope filter must survive.
	derived := wrapped.
		WithAttrs([]slog.Attr{slog.String("request_id", "abc")}).
		WithGroup("net")

	if _, ok := derived.(scopeFilterHandler); !ok {
		t.Fatalf("derived handler is %T, want scopeFilterHandler", derived)
	}

	ctx := context.Background()
	mkDebug := func(msg string) slog.Record {
		return slog.NewRecord(time.Now(), slog.LevelDebug, msg, 0)
	}

	if err := derived.Handle(ctx, mkDebug("failover: kept")); err != nil {
		t.Fatalf("Handle(failover) error: %v", err)
	}
	if err := derived.Handle(ctx, mkDebug("proxy: dropped")); err != nil {
		t.Fatalf("Handle(proxy) error: %v", err)
	}
	// A non-Debug record always passes, regardless of scope.
	infoRec := slog.NewRecord(time.Now(), slog.LevelInfo, "proxy: info always passes", 0)
	if err := derived.Handle(ctx, infoRec); err != nil {
		t.Fatalf("Handle(info) error: %v", err)
	}

	if len(capH.records) != 2 {
		t.Fatalf("expected 2 records (scoped debug + info), got %d: %+v", len(capH.records), capH.records)
	}
	for _, r := range capH.records {
		if r.Level == slog.LevelDebug && scopeOf(r.Message) != "failover" {
			t.Errorf("derived handler leaked an out-of-scope debug record: %q", r.Message)
		}
	}
}
