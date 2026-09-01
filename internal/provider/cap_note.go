package provider

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// What Model Hotel cannot know about a provider, made explicit. Ollama Cloud
// exposes no usage; a plain OpenAI-compatible endpoint exposes nothing at all.
// For those the only quota signal the gateway ever sees is the 429 whose body
// says the window or balance is spent. The cap ledger keeps the last of those
// per provider, so the quota badge can say "no usage API; last cap message:
// \"session usage limit\" at 14:51 (from a 429)" instead of staying blank.

// CapNote is the last exhausted 429 a provider answered: the phrase the
// classifier matched (the phrase-table key, never the body), the model that
// drew it, and when. Phrase is empty when the headers decided the class
// rather than a phrase. Entitled marks an exhaustion a person fixes (a spent
// balance, a plan) rather than a window that rolls over.
type CapNote struct {
	Phrase   string    `json:"phrase,omitempty"`
	Model    string    `json:"model"`
	Entitled bool      `json:"entitled,omitempty"`
	At       time.Time `json:"at"`
}

// CapLedger holds one CapNote per provider, in memory: a restart forgets them,
// and the next exhausted 429 writes them again. Bounded by the provider count.
type CapLedger struct {
	mu    sync.RWMutex
	notes map[uuid.UUID]CapNote
}

// NewCapLedger returns an empty ledger.
func NewCapLedger() *CapLedger {
	return &CapLedger{notes: make(map[uuid.UUID]CapNote)}
}

// Note records the provider's latest exhausted 429, replacing the previous one.
func (l *CapLedger) Note(providerID uuid.UUID, n CapNote) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.notes[providerID] = n
}

// Get returns the provider's last cap note, if it has answered an exhausted
// 429 since the process started.
func (l *CapLedger) Get(providerID uuid.UUID) (CapNote, bool) {
	if l == nil {
		return CapNote{}, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	n, ok := l.notes[providerID]
	return n, ok
}
