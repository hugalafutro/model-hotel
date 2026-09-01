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
	l.Note(a, CapNote{Phrase: "session usage limit", Model: "glm-5.3", Status: 429, At: t1})
	l.Note(b, CapNote{Model: "m", Status: 429, At: t1})
	l.Note(a, CapNote{Phrase: "weekly usage limit", Model: "glm-5.3", Status: 429, At: t1.Add(time.Hour)})
	got, ok := l.Get(a)
	if !ok || got.Phrase != "weekly usage limit" || !got.At.Equal(t1.Add(time.Hour)) {
		t.Errorf("note = %+v, %v; want the latest", got, ok)
	}
	all := l.All()
	if len(all) != 2 || all[b].Phrase != "" {
		t.Errorf("All = %+v", all)
	}
	all[a] = CapNote{}
	if n, _ := l.Get(a); n.Phrase != "weekly usage limit" {
		t.Error("All returned the live map")
	}

	var nilLedger *CapLedger
	nilLedger.Note(a, CapNote{})
	if _, ok := nilLedger.Get(a); ok || nilLedger.All() != nil {
		t.Error("a nil ledger is not inert")
	}
}
