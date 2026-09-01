package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// An exhausted 429 leaves its cap message on the provider's ledger: the phrase
// the classifier matched, the model that drew it, and when. A saturated one
// does not: it is not a quota reading.
func TestExhausted429_LeavesACapNote(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(ollamaExhaustedBody))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)
	if _, ok := env.Handler.CapLedger().Get(env.ProviderID); ok {
		t.Fatal("a fresh provider has a cap note")
	}

	if w := chatRequest(t, env); w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}
	note, ok := env.Handler.CapLedger().Get(env.ProviderID)
	if !ok || note.Phrase != "session usage limit" || note.Model != env.ModelName || note.Status != 429 || note.At.IsZero() {
		t.Errorf("cap note = %+v, %v; want the session usage limit phrase on %s", note, ok, env.ModelName)
	}
}

func TestSaturated429_LeavesNoCapNote(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"concurrent_budget_exceeded"}}`))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)
	chatRequest(t, env)
	if note, ok := env.Handler.CapLedger().Get(env.ProviderID); ok {
		t.Errorf("a saturated 429 left a cap note: %+v", note)
	}
}
