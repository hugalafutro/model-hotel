package logtest

import (
	"log/slog"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

func TestCapture_RecordsAndRestores(t *testing.T) {
	// A sentinel stands in for whatever handler a test file had installed, so
	// the restore is observed on the receiving end, not inferred.
	orig := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(orig) })
	sentinel := &Handler{}
	debuglog.SetHandler(sentinel)

	var released *Handler
	t.Run("captures", func(t *testing.T) {
		h := Capture(t)
		released = h
		debuglog.Warn("first", "n", 1)
		debuglog.Info("other")
		debuglog.Warn("first", "n", 2)

		if got := h.Count("first"); got != 2 {
			t.Fatalf("Count(first) = %d, want 2", got)
		}
		if r, ok := h.Find("first"); !ok || r.Level != slog.LevelWarn || r.Attrs["n"] != int64(1) {
			t.Errorf("Find(first) = %+v, %v; want the Warn with n=1", r, ok)
		}
		if r, ok := h.Last("first"); !ok || r.Attrs["n"] != int64(2) {
			t.Errorf("Last(first) = %+v, %v; want the record with n=2", r, ok)
		}
		if got := h.Attr("first", "n"); got != int64(1) {
			t.Errorf("Attr(first, n) = %v, want 1", got)
		}
		if h.Attr("first", "missing") != nil || h.Attr("absent", "n") != nil {
			t.Error("Attr must be nil for a missing key or message")
		}
		if !h.Has("other") || h.Has("absent") {
			t.Error("Has must track exactly the captured messages")
		}
		if r, ok := h.Latest(); !ok || r.Msg != "first" || r.Attrs["n"] != int64(2) {
			t.Errorf("Latest() = %+v, %v; want the last record logged", r, ok)
		}
		if got := len(h.Records()); got != 3 {
			t.Errorf("Records() has %d entries, want 3", got)
		}
	})

	// The subtest's cleanup ran: the handler installed before Capture is back
	// and the released recorder hears nothing more.
	debuglog.Warn("after cleanup")
	if !sentinel.Has("after cleanup") {
		t.Error("the handler installed before Capture did not get the record logged after cleanup")
	}
	if released.Has("after cleanup") {
		t.Error("a record logged after cleanup reached the released capture handler")
	}
	if sentinel.Has("first") {
		t.Error("a record logged during Capture leaked to the handler it displaced")
	}
}
