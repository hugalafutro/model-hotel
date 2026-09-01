package provider

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// One note per provider, the latest wins, and a nil ledger is inert.
func TestCapLedger(t *testing.T) {
	l := NewCapLedger()
	a, b := uuid.New(), uuid.New()
	if _, ok := l.Get(a); ok {
		t.Fatal("an empty ledger has a note")
	}
	t1 := time.Date(2026, 8, 31, 14, 51, 0, 0, time.UTC)
	l.Note(a, CapNote{Phrase: "session usage limit", Model: "glm-5.3", At: t1})
	l.Note(b, CapNote{Model: "m", Entitled: true, At: t1})
	l.Note(a, CapNote{Phrase: "weekly usage limit", Model: "glm-5.3", At: t1.Add(time.Hour)})
	got, ok := l.Get(a)
	if !ok || got.Phrase != "weekly usage limit" || !got.At.Equal(t1.Add(time.Hour)) {
		t.Errorf("note = %+v, %v; want the latest", got, ok)
	}
	if n, ok := l.Get(b); !ok || !n.Entitled || n.Phrase != "" {
		t.Errorf("other provider's note = %+v, %v", n, ok)
	}

	var nilLedger *CapLedger
	nilLedger.Note(a, CapNote{})
	if _, ok := nilLedger.Get(a); ok {
		t.Error("a nil ledger is not inert")
	}
}
