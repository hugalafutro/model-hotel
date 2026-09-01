package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !ok || note.Phrase != "session usage limit" || note.Model != env.ModelName || note.Entitled || note.At.IsZero() {
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

// With failover on 429 switched off the 429 is forwarded untouched, and the
// ledger and the counter still see it: a routing choice must not blind the
// badge that exists for providers nothing else reports on.
func TestExhausted429_NotedWhenFailoverOn429IsOff(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(ollamaExhaustedBody))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)
	withRateLimitFailover(t, env.Handler, "false")
	series := `modelhotel_upstream_rate_limit_total{class="exhausted",model="` + env.ModelName + `",provider="` + env.ProviderName + `"}`
	before := metricValue(t, series)

	// The 429 is answered as it was before the ledger existed: the status
	// stands, and the gateway's own error text, never the provider's body.
	w := chatRequest(t, env)
	if w.Code != http.StatusTooManyRequests || strings.Contains(w.Body.String(), "session usage limit") {
		t.Fatalf("response = %d %q, want the 429 with the gateway's text", w.Code, w.Body.String())
	}
	note, ok := env.Handler.CapLedger().Get(env.ProviderID)
	if !ok || note.Phrase != "session usage limit" || note.Model != env.ModelName {
		t.Errorf("cap note with failover on 429 off = %+v, %v", note, ok)
	}
	if got := metricValue(t, series) - before; got != 1 {
		t.Errorf("exhausted 429s counted with failover on 429 off = %v, want 1", got)
	}
}
