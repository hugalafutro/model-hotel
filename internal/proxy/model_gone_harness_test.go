package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// The retirement suite's harness: the scripted upstream every probe test
// answers with, the streak readers, and the candidate/handler builders.

// goneAPIKey is the decrypted credential the fixture candidates carry. The
// probe authenticates exactly like real traffic, so a candidate without one is
// not the candidate production hands to noteModelGone.
const goneAPIKey = "sk-gone-fixture-key"

// waitForDisable polls the mock for a recorded SetEnabled call, since
// noteModelGone disables on a detached goroutine so the request path is not
// blocked by the write.
func waitForDisable(t *testing.T, repo *mockModelRepo) []setEnabledCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := repo.disableCalls(); len(calls) > 0 {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	return repo.disableCalls()
}

// goneStreakFor returns the model's live streak, failing the test if there is
// none. The postponement paths leave a streak behind rather than a write, so
// several tests below assert on it directly.
func goneStreakFor(t *testing.T, h *Handler, id uuid.UUID, endpoint string) *goneStreak {
	t.Helper()
	raw, ok := h.goneStrikes.Load(goneStreakKey{model: id, endpoint: endpoint})
	if !ok {
		t.Fatal("the model has no streak")
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		t.Fatal("unexpected streak type")
	}
	return streak
}

// waitForStreakCount blocks until the model's streak holds want strikes, for the
// paths that reset the count in place instead of dropping the entry. The reset
// happens on the detached goroutine, so reading the count straight after the
// refusals that triggered it races the reset rather than observing it.
func waitForStreakCount(t *testing.T, h *Handler, id uuid.UUID, endpoint string, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, ok := h.goneStrikes.Load(goneStreakKey{model: id, endpoint: endpoint}); ok {
			if s, ok := raw.(*goneStreak); ok && s.count() == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the streak never settled at %d strikes", want)
}

// probeClaimAt reports when the model's streak will next admit a probe. The zero
// time means no claim has been spent on this surface.
func probeClaimAt(t *testing.T, h *Handler, id uuid.UUID, endpoint string) time.Time {
	t.Helper()
	streak := goneStreakFor(t, h, id, endpoint)
	streak.mu.Lock()
	defer streak.mu.Unlock()
	return streak.nextProbeAt
}

// expireProbeCooldown ages the model's probe claim so the next refusal may spend
// a probe, standing in for goneProbeCooldown having elapsed. Driven through the
// streak's own field, as the staleness test drives lastStrike: a knob or an
// injected clock would put a seam in production code that exists only for tests.
func expireProbeCooldown(t *testing.T, h *Handler, id uuid.UUID, endpoint string) {
	t.Helper()
	streak := goneStreakFor(t, h, id, endpoint)
	streak.mu.Lock()
	streak.nextProbeAt = time.Now().Add(-time.Second)
	streak.mu.Unlock()
}

// waitForInconclusiveRun blocks until the model's streak reports want probes in
// a row that established nothing. The verdict is applied on the detached probe
// goroutine, so reading the run straight after the refusal that triggered it
// races the update.
func waitForInconclusiveRun(t *testing.T, h *Handler, id uuid.UUID, endpoint string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		streak := goneStreakFor(t, h, id, endpoint)
		streak.mu.Lock()
		got = streak.inconclusive
		streak.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("inconclusive run settled at %d, want %d", got, want)
}

// waitForProbes blocks until the fake provider has been asked n times, so a
// test can assert on what a probe did rather than on how long it took. Without
// it, "no second probe was sent" and "the second probe has not been sent yet"
// are the same observation.
func waitForProbes(t *testing.T, script *goneScriptedServer, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if paths := script.requestedPaths(); len(paths) >= n {
			return paths
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d probe(s), got %v", n, script.requestedPaths())
	return nil
}

// goneRefusalServer starts a fake provider that refuses modelID as retired on
// every request: a 404 naming the model beside a phrase from modelGoneVerbs,
// which classifyUpstreamError reads as KindProviderModelGone.
//
// The tests below are about what the disable machinery does once the evidence is
// in — the threshold, the two cancellation windows, the rollback, the
// streak-identity rules — and the probe adjudicates every retirement, so giving
// it nothing to reach would turn each of them into an assertion about an
// unreachable provider instead.
func goneRefusalServer(t *testing.T, modelID string) *httptest.Server {
	t.Helper()
	body := goneRefusalBody(modelID)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// goneRefusalBody is the retirement refusal itself, split out from the server so
// a test that needs its own server can still produce the one body
// classifyUpstreamError reads as KindProviderModelGone. The model id has to look
// like one (looksLikeAModelID wants a digit or a hyphen) or the classifier will
// not attribute the phrase to it.
func goneRefusalBody(modelID string) string {
	return fmt.Sprintf("{\"error\":{\"message\":\"The model `%s` does not exist\"}}", modelID)
}

// goneServedAnswer is a real completion: a 200 carrying visible content, which
// is what probeDeliveredContent requires before it will call a probe served.
// Deliberately not an empty 200 — that is inconclusive, not a success.
const goneServedAnswer = `{"id":"chatcmpl-probe","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"completion_tokens":1}}`

// goneRateLimitedAnswer is the provider incident the postponement exists for: a
// 429 says nothing whatever about whether the model still exists.
const goneRateLimitedAnswer = `{"error":{"message":"Rate limit reached for requests","type":"requests"}}`

// goneScriptedServer is a fake provider whose answer a test can change while the
// test is running, and which records every path it was asked for.
//
// Neither half is served by the fixtures already here: goneRefusalServer only
// ever refuses, and the candidate is built once and carries its base URL, so a
// postponement test cannot switch to a second server. probeServer
// (model_probe_test.go) keeps only the LAST path, whereas the family tests
// assert over all of them.
type goneScriptedServer struct {
	mu     sync.Mutex
	status int
	body   string
	paths  []string
}

// answer replaces what the server returns from the next request onwards.
func (s *goneScriptedServer) answer(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

// requestedPaths returns a copy of every path the server was asked for, in
// order. Copied under the lock: the probe runs on a detached goroutine, so the
// server's handler may be appending while a test reads.
func (s *goneScriptedServer) requestedPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// newGoneScriptedServer starts the fake provider with its opening answer.
func newGoneScriptedServer(t *testing.T, status int, body string) (*httptest.Server, *goneScriptedServer) {
	t.Helper()
	script := &goneScriptedServer{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		script.mu.Lock()
		status, body := script.status, script.body
		script.paths = append(script.paths, r.URL.Path)
		script.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, script
}

// goneCandidateFor builds the candidate the retirement path actually carries,
// pointed at a provider that refuses this model. It is the whole candidate and
// not just the model because the probe needs a base URL, a provider name and a
// decrypted key to ask anything at all.
func goneCandidateFor(t *testing.T, m *model.Model, providerName string) modelCandidate {
	t.Helper()
	return goneCandidateAt(m, providerName, goneRefusalServer(t, m.ModelID).URL)
}

// goneCandidateAt is the same candidate pointed at a base URL the test chose,
// for the cases whose whole subject is what the provider answers.
func goneCandidateAt(m *model.Model, providerName, baseURL string) modelCandidate {
	return modelCandidate{
		model:    m,
		provider: &provider.Provider{ID: uuid.New(), Name: providerName, BaseURL: baseURL},
		apiKey:   goneAPIKey,
	}
}

// newGoneHandler builds a Handler carrying a real shared upstream transport,
// which is what production has and what the pre-retirement probe now goes out
// over.
func newGoneHandler(t *testing.T, repo *mockModelRepo) *Handler {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return &Handler{modelRepo: repo, upstreamTransport: tr}
}
