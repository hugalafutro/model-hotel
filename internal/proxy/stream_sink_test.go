package proxy

import (
	"net/http/httptest"
	"testing"
)

// TestStreamSink covers the sink's framing, byte accounting, and the
// swallowBlank separator flag (Phase 5). httptest.NewRecorder implements
// http.Flusher, so canFlush is true.
func TestStreamSink(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	s := newStreamSink(rec)
	if !s.canFlush {
		t.Fatal("recorder should support flushing")
	}

	// write() forwards raw bytes verbatim, accounts them, and does NOT set
	// swallowBlank (used for comment/blank/[DONE]/plain-forward).
	if err := s.write([]byte("data: raw")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if s.bytesWritten != 9 {
		t.Errorf("bytesWritten = %d, want 9", s.bytesWritten)
	}
	if s.swallowBlank {
		t.Error("write() must not set swallowBlank")
	}

	// writeData() frames as "data: <payload>\n\n" and auto-sets swallowBlank
	// (every data emit owns its separator, so the upstream's trailing blank
	// is swallowed by the orchestrator's blank handler).
	before := s.bytesWritten
	if err := s.writeData([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("writeData: %v", err)
	}
	if !s.swallowBlank {
		t.Error("writeData() must set swallowBlank")
	}
	wantData := "data: " + `{"a":1}` + "\n\n"
	if int(s.bytesWritten-before) != len(wantData) {
		t.Errorf("writeData wrote %d bytes, want %d", s.bytesWritten-before, len(wantData))
	}

	s.flush() // must not panic

	if got, want := rec.Body.String(), "data: raw"+wantData; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
