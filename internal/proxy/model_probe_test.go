package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// probeAPIKey is the credential the fake provider expects to see. The probe
// runs off the request path, so nothing else would notice if it stopped
// authenticating; the served test asserts the header explicitly for that
// reason.
const probeAPIKey = "sk-probe-fixture-key"

// capturedProbe records what the fake provider was actually asked for. Guarded
// because the handler runs on the server's goroutine.
type capturedProbe struct {
	mu     sync.Mutex
	path   string
	auth   string
	body   []byte
	called int
}

func (c *capturedProbe) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = r.URL.Path
	c.auth = r.Header.Get("Authorization")
	c.body = body
	c.called++
}

func (c *capturedProbe) snapshot() (path, auth string, body []byte, called int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path, c.auth, c.body, c.called
}

// probeServer starts a fake provider that answers every request with the given
// status and body, recording what it was asked for.
func probeServer(t *testing.T, status int, body string) (*httptest.Server, *capturedProbe) {
	t.Helper()
	rec := &capturedProbe{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// probeCandidateFor builds a candidate pointed at the fake provider. It carries
// a decrypted key exactly as the real one does, so the probe needs no database.
func probeCandidateFor(baseURL, modelID string) modelCandidate {
	return modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: modelID},
		provider: &provider.Provider{ID: uuid.New(), Name: "Probe Provider", BaseURL: baseURL},
		apiKey:   probeAPIKey,
	}
}

// newProbeHandler builds a Handler carrying a real shared upstream transport,
// which is what production has. Most cases use it so the probe is exercised
// over the same egress machinery real traffic uses rather than over the nil
// fallback.
func newProbeHandler(t *testing.T) *Handler {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return &Handler{upstreamTransport: tr}
}

// runProbe drives the real helper under the production budget. Deliberately
// goneProbeTimeout rather than a number picked for the tests: probeModel takes
// its deadline from the caller, so a case that only passes with more (or less)
// time than the retirement path actually grants would be pinning something the
// gateway never does.
func runProbe(t *testing.T, h *Handler, candidate modelCandidate, endpointType string) probeVerdict {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), goneProbeTimeout)
	defer cancel()
	return h.probeModel(ctx, candidate, endpointType)
}

// TestProbeModel_ServedAnswerIsNotARetirement is the case the whole feature
// turns on: a model that answers must never be retired, whatever the classifier
// made of the three refusals that nominated it.
//
// It also pins that the probe went out over the REAL egress path rather than
// some easier bespoke request — same endpoint, same auth header, same resolved
// model id, and the documented output budget. A probe that is cheaper to
// satisfy than real traffic would clear models that real traffic cannot reach.
func TestProbeModel_ServedAnswerIsNotARetirement(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"Hi"}}]}`)
	h := newProbeHandler(t)
	candidate := probeCandidateFor(srv.URL, "gemini-2.0-flash")

	if got := runProbe(t, h, candidate, endpointTypeChat); got != probeServed {
		t.Fatalf("verdict = %s, want served", got)
	}

	path, auth, body, called := rec.snapshot()
	if called != 1 {
		t.Fatalf("provider was called %d times, want exactly 1", called)
	}
	if path != "/chat/completions" {
		t.Errorf("probe hit %q, want /chat/completions", path)
	}
	if auth != "Bearer "+probeAPIKey {
		t.Errorf("authorization header = %q, want the provider's key", auth)
	}

	var sent struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("probe body is not JSON: %v", err)
	}
	if sent.Model != "gemini-2.0-flash" {
		t.Errorf("probe asked for %q, want the candidate's upstream model id", sent.Model)
	}
	if sent.MaxTokens != goneProbeMaxTokens {
		t.Errorf("max_tokens = %d, want %d", sent.MaxTokens, goneProbeMaxTokens)
	}
	if len(sent.Messages) != 1 || sent.Messages[0].Role != "user" {
		t.Errorf("probe body carried %+v, want a single user message", sent.Messages)
	}
}

// TestProbeModel_ReasoningModelServedOnUsageAlone covers the model that spends
// its whole budget thinking and returns no visible text. Judging on content
// alone would call that empty and postpone every retirement decision for the
// entire reasoning family forever.
func TestProbeModel_ReasoningModelServedOnUsageAlone(t *testing.T) {
	t.Parallel()

	srv, _ := probeServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":""}}],"usage":{"completion_tokens":12}}`)
	h := newProbeHandler(t)

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "gpt-5.6-sol"), endpointTypeChat); got != probeServed {
		t.Fatalf("verdict = %s, want served", got)
	}
}

// TestProbeModel_EmptyAnswerPostpones pins the half of the judgement an earlier
// review round caught on the streaming path: a 200 that carries nothing is not
// a success. It is not a refusal either, so it must postpone rather than
// retire — the model gets asked again next time.
func TestProbeModel_EmptyAnswerPostpones(t *testing.T) {
	t.Parallel()

	srv, _ := probeServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":""}}]}`)
	h := newProbeHandler(t)

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "glm-5.2"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeModel_NoTransportPostpones pins that the probe refuses to fall back
// to net/http's default client.
//
// The nil check cannot go away — upstreamTransport is a concrete
// *http.Transport, so assigning a nil one panics inside RoundTrip rather than
// falling back — but leaving Transport unset is not a safe way to satisfy it: an
// unset Transport is http.DefaultTransport, which has no DialContext, so the
// SafeDialer's guard against a provider URL that resolves into the gateway's own
// network would be silently gone for the one request nobody is watching. The
// probe postpones instead, which is what it does with every other question it
// cannot answer safely.
//
// The fake provider records every call, so this asserts the request was never
// sent rather than merely that the verdict came out inconclusive — which it
// would either way.
func TestProbeModel_NoTransportPostpones(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusOK, `{"choices":[{"message":{"content":"Hi"}}]}`)
	h := &Handler{}

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "glm-5.2"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
	if _, _, _, called := rec.snapshot(); called != 0 {
		t.Fatalf("the provider was called %d times over an unguarded transport", called)
	}
}

// TestProbeModel_RefusalByNameRetires is the other half: the provider naming
// this model and saying it does not exist is the one thing that may retire it.
func TestProbeModel_RefusalByNameRetires(t *testing.T) {
	t.Parallel()

	srv, _ := probeServer(t, http.StatusNotFound, "{\"error\":{\"message\":\"The model `gemini-2.0-flash` does not exist\"}}")
	h := newProbeHandler(t)

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "gemini-2.0-flash"), endpointTypeChat); got != probeRefused {
		t.Fatalf("verdict = %s, want refused", got)
	}
}

// TestProbeModel_RateLimitPostpones is the line that keeps a provider incident
// from becoming a mass retirement. A 429 is a live model the gateway may not
// have right now.
func TestProbeModel_RateLimitPostpones(t *testing.T) {
	t.Parallel()

	srv, _ := probeServer(t, http.StatusTooManyRequests, `{"error":{"message":"Rate limit reached for gemini-2.0-flash"}}`)
	h := newProbeHandler(t)

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "gemini-2.0-flash"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeModel_EntitlementFailurePostpones pins that "you cannot pay for this
// model" is never "this model is gone". Both shapes the classifier recognises
// as an entitlement failure must leave the model enabled: an operator topping
// up their account must not find their catalog retired behind them.
func TestProbeModel_EntitlementFailurePostpones(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"payment required", http.StatusPaymentRequired, `{"error":{"message":"quota exhausted"}}`},
		{"insufficient balance inside a 429", http.StatusTooManyRequests, `{"error":{"code":1113,"message":"Insufficient balance or no resource package. Please recharge."}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := probeServer(t, tc.status, tc.body)
			h := newProbeHandler(t)
			if got := runProbe(t, h, probeCandidateFor(srv.URL, "glm-4.6"), endpointTypeChat); got != probeInconclusive {
				t.Fatalf("verdict = %s, want inconclusive", got)
			}
		})
	}
}

// TestProbeModel_UnreachableProviderPostpones covers the failure mode that
// would be most destructive: a network problem on the gateway's own side must
// not read as every model being gone.
func TestProbeModel_UnreachableProviderPostpones(t *testing.T) {
	t.Parallel()

	// A privileged port on loopback: binding it needs root, so no test server
	// can ever be listening there and the connection is refused immediately.
	// Deliberately not "start a server and close it" — that releases the port
	// back to the ephemeral range, where the OS can hand it straight to another
	// parallel test's server and this probe would then reach a live listener.
	//
	// A real transport, so the refused connection is what produces the verdict.
	// On a bare Handler the probe now postpones before it dials at all, and this
	// test would pass without a packet being sent.
	h := newProbeHandler(t)
	if got := runProbe(t, h, probeCandidateFor("http://127.0.0.1:1", "gemini-2.0-flash"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeModel_DeadlinePostpones pins that the probe's own budget expiring is
// evidence about the budget, not about the model.
func TestProbeModel_DeadlinePostpones(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	h := newProbeHandler(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if got := h.probeModel(ctx, probeCandidateFor(srv.URL, "gemini-2.0-flash"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeModel_EmbeddingsProbeUsesItsOwnEndpoint pins that an embeddings
// model is asked something an embeddings model can answer. Probing it with a
// chat body would fail for reasons that have nothing to do with retirement, and
// that failure would read as confirmation of the retirement.
func TestProbeModel_EmbeddingsProbeUsesItsOwnEndpoint(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusOK, `{"data":[{"embedding":[0.1,0.2,0.3]}]}`)
	h := newProbeHandler(t)

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "text-embedding-3-small"), endpointTypeEmbeddings); got != probeServed {
		t.Fatalf("verdict = %s, want served", got)
	}

	path, _, body, _ := rec.snapshot()
	if path != "/embeddings" {
		t.Fatalf("probe hit %q, want /embeddings", path)
	}

	var sent struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("probe body is not JSON: %v", err)
	}
	if sent.Input == "" {
		t.Errorf("embeddings probe body carried no input: %s", body)
	}
	if sent.Model != "text-embedding-3-small" {
		t.Errorf("probe asked for %q, want the candidate's upstream model id", sent.Model)
	}
}

// TestProbeEndpointForFamily pins which families may be auto-retired at all.
// The unprobeable ones are refused deliberately: image, TTS and STT probes cost
// real money and would fail for reasons unrelated to retirement, and an unknown
// family is something nothing can be substantiated about.
func TestProbeEndpointForFamily(t *testing.T) {
	t.Parallel()

	cases := []struct {
		family   string
		endpoint string
		ok       bool
	}{
		{endpointTypeChat, "/chat/completions", true},
		{endpointTypeMessages, "/chat/completions", true},
		{endpointTypeEmbeddings, "/embeddings", true},
		{endpointTypeRerank, "", false},
		{endpointTypeImage, "", false},
		{endpointTypeTTS, "", false},
		{endpointTypeSTT, "", false},
		{"", "", false},
		{"something-new", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.family, func(t *testing.T) {
			t.Parallel()
			endpoint, ok := probeEndpointForFamily(tc.family)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if endpoint != tc.endpoint {
				t.Errorf("endpoint = %q, want %q", endpoint, tc.endpoint)
			}
		})
	}
}

// TestProbeModel_OpenCircuitCostsNoRequest pins that the probe asks the breaker
// before it asks the provider.
//
// The probe skips the breaker's ACCOUNTING deliberately: charging a provider's
// circuit for a verification the operator did not request would let the
// verification itself take a healthy provider out of routing. Skipping the
// CHECK is a different thing entirely, and it was never argued — a probe to a
// provider the gateway has already sidelined is a guaranteed-wasted call to a
// host nothing else is being sent to, and its answer postpones anyway. The
// fixture refuses the model by name, so a probe that went out would RETIRE it;
// the whole assertion is that no request is made and the retirement postpones.
func TestProbeModel_OpenCircuitCostsNoRequest(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusNotFound, `{"error":{"message":"The model `+"`claude-sonnet-4`"+` does not exist"}}`)
	h := newProbeHandler(t)
	h.circuitBreaker = failover.NewCircuitBreaker(nil)
	cand := probeCandidateFor(srv.URL, "claude-sonnet-4")

	// Driven through the breaker's own API rather than by reaching into its
	// state: the check has to agree with what the routing path would see, and
	// the threshold is the breaker's to define.
	for range h.circuitBreaker.Threshold {
		h.circuitBreaker.RecordFailure(cand.provider.ID, cand.provider.Name, "")
	}
	if state := h.circuitBreaker.GetState(cand.provider.ID, ""); state != failover.StateOpen {
		t.Fatalf("fixture: the circuit did not open, state = %v", state)
	}

	if got := runProbe(t, h, cand, endpointTypeChat); got != probeInconclusive {
		t.Errorf("a probe to a sidelined provider must establish nothing, got %v", got)
	}
	if _, _, _, called := rec.snapshot(); called != 0 {
		t.Errorf("a sidelined provider must not be asked, got %d request(s)", called)
	}
}

// TestProbeVerdictString pins that an unhandled verdict is named as one.
//
// noteModelGone's default arm exists specifically to surface a verdict nobody
// wrote a case for, and it fails the retirement closed and logs this string.
// While String's default returned "inconclusive", the one diagnostic designed
// to name the unknown value was guaranteed to misname it as the zero value it
// is not — so a fourth verdict silently postponing every retirement for a whole
// class of models would have read, in the logs, exactly like an ordinary
// unproven probe. The unknown case is the only reason this test exists; the
// three named ones are here so a renumbering cannot quietly shuffle them.
func TestProbeVerdictString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		verdict probeVerdict
		want    string
	}{
		{probeInconclusive, "inconclusive"},
		{probeRefused, "refused"},
		{probeServed, "served"},
		{probeVerdict(7), "unknown(7)"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.verdict.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProbeModel_UnprobeableFamilyCostsNoRequest is the defensive half of the
// gate. The caller is expected to check the family first; if it does not, the
// probe must still refuse to send anything — an image model asked for a chat
// completion would answer with a failure that looks exactly like a retirement.
func TestProbeModel_UnprobeableFamilyCostsNoRequest(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusOK, `{"choices":[{"message":{"content":"Hi"}}]}`)
	h := &Handler{}

	if got := runProbe(t, h, probeCandidateFor(srv.URL, "dall-e-3"), endpointTypeImage); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
	if _, _, _, called := rec.snapshot(); called != 0 {
		t.Fatalf("provider was called %d times, want none", called)
	}
}

// TestProbeModel_NilCandidateIsInconclusive pins that a malformed candidate
// postpones instead of panicking on the detached disable goroutine, where a
// panic would take the process down.
func TestProbeModel_NilCandidateIsInconclusive(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	if got := runProbe(t, h, modelCandidate{}, endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeDeliveredContent pins what counts as the model having worked. Each
// case is a real provider shape: reasoning-only answers, tool calls with no
// prose, and bodies that do not parse at all.
func TestProbeDeliveredContent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		family string
		body   string
		flowed bool
	}{
		{"visible content", endpointTypeChat, `{"choices":[{"message":{"content":"Hi"}}]}`, true},
		{"reasoning content only", endpointTypeChat, `{"choices":[{"message":{"content":"","reasoning_content":"thinking"}}]}`, true},
		{"reasoning field only", endpointTypeChat, `{"choices":[{"message":{"content":"","reasoning":"thinking"}}]}`, true},
		{"tool calls only", endpointTypeChat, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`, true},
		{"completion tokens only", endpointTypeChat, `{"choices":[],"usage":{"completion_tokens":7}}`, true},
		// Content is `any` on the wire: a provider may answer with content parts
		// instead of a string. Judging only the string shape calls such a model
		// empty, and the probe then returns inconclusive on a model that is
		// demonstrably alive — the streak is never parked, and every fresh
		// refusal past the cooldown buys another upstream request indefinitely.
		{"content parts", endpointTypeChat, `{"choices":[{"message":{"content":[{"type":"text","text":"Hi"}]}}]}`, true},
		{"empty content parts", endpointTypeChat, `{"choices":[{"message":{"content":[]}}]}`, false},
		{"nothing at all", endpointTypeChat, `{"choices":[]}`, false},
		{"unparseable", endpointTypeChat, `<html>502 Bad Gateway</html>`, false},
		{"embedding vector", endpointTypeEmbeddings, `{"data":[{"embedding":[0.5]}]}`, true},
		// A provider may answer with the vector base64-encoded, which is a JSON
		// string rather than an array. Decoding it as []float64 made the whole
		// document fail to parse, so a live embeddings model came back
		// inconclusive, its streak was never parked, and every refusal past the
		// cooldown bought another upstream request indefinitely.
		{"base64 embedding", endpointTypeEmbeddings, `{"data":[{"embedding":"eNqLZoAAAA=="}]}`, true},
		{"empty embedding vector", endpointTypeEmbeddings, `{"data":[{"embedding":[]}]}`, false},
		{"empty base64 embedding", endpointTypeEmbeddings, `{"data":[{"embedding":""}]}`, false},
		// Emptiness is judged structurally, not against the spellings
		// encoding/json happens to emit: a provider whose encoder pads still
		// answered with nothing.
		{"padded empty vector", endpointTypeEmbeddings, `{"data":[{"embedding":[ ]}]}`, false},
		{"newline-padded empty vector", endpointTypeEmbeddings, "{\"data\":[{\"embedding\":[\n]}]}", false},
		{"padded empty base64", endpointTypeEmbeddings, `{"data":[{"embedding":"  "}]}`, false},
		{"null embedding", endpointTypeEmbeddings, `{"data":[{"embedding":null}]}`, false},
		{"embedding key absent", endpointTypeEmbeddings, `{"data":[{}]}`, false},
		{"no embedding data", endpointTypeEmbeddings, `{"data":[]}`, false},
		{"unparseable embeddings", endpointTypeEmbeddings, `<html>502 Bad Gateway</html>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := probeDeliveredContent(tc.family, []byte(tc.body)); got != tc.flowed {
				t.Fatalf("probeDeliveredContent = %v, want %v", got, tc.flowed)
			}
		})
	}
}

// dialToTestServer builds an upstream transport that sends every connection to
// the given test server regardless of the hostname in the URL. Nothing about
// the probe is faked: it is the production *http.Transport field, carrying a
// DialContext, which is how the real one reaches providers too. The hostname
// still has to be Google's, because that is what LegacyTypeFromURL reads to
// decide a candidate is vertex-express.
func dialToTestServer(t *testing.T, srv *httptest.Server) *http.Transport {
	t.Helper()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}
	t.Cleanup(tr.CloseIdleConnections)
	return tr
}

// TestProbeModel_VertexExpressProbeSpeaksGemini is why the probe goes through
// the real egress builder instead of assembling its own request. A
// vertex-express candidate is served the native generateContent route, so an
// OpenAI-shaped chat probe would hit a path Google does not serve and come back
// with a failure that says nothing about the model — and gemini-2.0-flash, the
// model this whole feature was built for, is exactly that kind of candidate.
func TestProbeModel_VertexExpressProbeSpeaksGemini(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusOK, `{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]},"finishReason":"STOP"}]}`)
	h := &Handler{upstreamTransport: dialToTestServer(t, srv)}
	candidate := probeCandidateFor("http://us-central1-aiplatform.googleapis.com/v1", "gemini-2.0-flash")

	if got := runProbe(t, h, candidate, endpointTypeChat); got != probeServed {
		t.Fatalf("verdict = %s, want served", got)
	}

	path, _, _, called := rec.snapshot()
	if called != 1 {
		t.Fatalf("provider was called %d times, want exactly 1", called)
	}
	if path != "/v1/publishers/google/models/gemini-2.0-flash:generateContent" {
		t.Errorf("probe hit %q, want the native generateContent route", path)
	}
}

// TestProbeModel_UntranslatableDialectAnswerPostpones pins the other half of
// that re-route: a 200 whose body cannot be read as the dialect it claims to
// speak proves nothing, so it must postpone rather than count as a refusal.
func TestProbeModel_UntranslatableDialectAnswerPostpones(t *testing.T) {
	t.Parallel()

	srv, _ := probeServer(t, http.StatusOK, `<html>502 Bad Gateway</html>`)
	h := &Handler{upstreamTransport: dialToTestServer(t, srv)}
	candidate := probeCandidateFor("http://us-central1-aiplatform.googleapis.com/v1", "gemini-2.0-flash")

	if got := runProbe(t, h, candidate, endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeModel_UnbuildableRequestPostpones covers a provider whose base URL
// cannot form a valid target URL. The gateway's own misconfiguration must not
// be charged to the model: a request that never left is not evidence of
// anything.
func TestProbeModel_UnbuildableRequestPostpones(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	// A newline is rejected by net/url, so the request cannot be constructed.
	candidate := probeCandidateFor("http://127.0.0.1:1/v1\n", "gemini-2.0-flash")

	if got := runProbe(t, h, candidate, endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// TestProbeModel_RedirectGuardAppliesToTheProbe pins that running off the
// request path does not drop the SSRF defenses real traffic gets. A provider
// that answers the probe with a redirect must not be able to walk the
// decrypted key to another host, and the refused hop must postpone rather than
// retire.
func TestProbeModel_RedirectGuardAppliesToTheProbe(t *testing.T) {
	t.Parallel()

	target, targetRec := probeServer(t, http.StatusOK, `{"choices":[{"message":{"content":"Hi"}}]}`)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/chat/completions", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)

	h := newProbeHandler(t)
	h.safeDialer = NewSafeDialer(nil, nil)
	if got := runProbe(t, h, probeCandidateFor(redirector.URL, "gemini-2.0-flash"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
	if _, _, _, called := targetRec.snapshot(); called != 0 {
		t.Fatalf("the redirect target was called %d times, want none", called)
	}
}

// TestProbeModel_LearnedParamRenameIsApplied is the invariant that justifies
// going through buildCandidateRequest at all: the probe must not be able to
// fail where real traffic succeeds.
//
// OpenAI's reasoning models reject max_tokens and demand
// max_completion_tokens, which the gateway learns from a 400 and caches. Real
// traffic to such a model always carries that rename, because every request
// goes through paramrewrite.BuildUpstreamBody. A probe that skipped it would
// send the exact parameter the model rejects, collect a 400, classify it as
// not-a-retirement and postpone forever — permanently unable to CLEAR the
// reasoning family the 64-token budget was chosen for.
func TestProbeModel_LearnedParamRenameIsApplied(t *testing.T) {
	t.Parallel()

	srv, rec := probeServer(t, http.StatusOK, `{"choices":[{"message":{"content":"Hi"}}]}`)
	h := newProbeHandler(t)
	cand := probeCandidateFor(srv.URL, "gpt-5.6-sol")
	// Seeded through the production writer, keyed exactly as BuildUpstreamBody
	// keys it: the learning is scoped to the individual provider, so the seed
	// must name this candidate's provider rather than its type.
	paramrewrite.MergeLearnedParamCache(&h.paramRenameCache,
		paramrewrite.LearnedCacheKey(cand.provider.ID.String(), "gpt-5.6-sol"),
		map[string]string{
			"max_tokens": "max_completion_tokens",
		})

	if got := runProbe(t, h, cand, endpointTypeChat); got != probeServed {
		t.Fatalf("verdict = %s, want served", got)
	}

	_, _, body, _ := rec.snapshot()
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("probe body is not JSON: %v", err)
	}
	if _, stillThere := sent["max_tokens"]; stillThere {
		t.Errorf("probe sent max_tokens, which this model rejects: %s", body)
	}
	budget, renamed := sent["max_completion_tokens"]
	if !renamed {
		t.Fatalf("probe body carried no max_completion_tokens: %s", body)
	}
	if budget != float64(goneProbeMaxTokens) {
		t.Errorf("renamed budget = %v, want %d", budget, goneProbeMaxTokens)
	}
	if sent["model"] != "gpt-5.6-sol" {
		t.Errorf("probe asked for %v, want the candidate's upstream model id", sent["model"])
	}
}

// TestProbeModel_TruncatedRefusalPostpones pins the one place a discarded read
// error could RETIRE a model. The provider declares more body than it delivers
// and the connection dies mid-answer; the surviving prefix happens to name the
// model beside a gone-phrase, so classifying it would read exactly like a real
// refusal. A body that could not be read to the end is not the provider saying
// anything.
func TestProbeModel_TruncatedRefusalPostpones(t *testing.T) {
	t.Parallel()

	refusal := "{\"error\":{\"message\":\"The model `gemini-2.0-flash` does not exist\"}}"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Promise far more than is written, then return: the server closes the
		// connection and the client's read fails with an unexpected EOF.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(refusal)*4))
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, refusal)
	}))
	t.Cleanup(srv.Close)

	h := newProbeHandler(t)
	if got := runProbe(t, h, probeCandidateFor(srv.URL, "gemini-2.0-flash"), endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
}

// paddedRefusal builds a retirement refusal of roughly size bytes: the
// gone-phrase first, so the classifier sees it inside its own 10 000-character
// window whatever the total length, then filler.
//
// The point is a body whose CLASSIFICATION is identical at every size, so a
// verdict that changes with the size changed because of the size.
func paddedRefusal(modelID string, size int) string {
	head := fmt.Sprintf("{\"error\":{\"message\":\"The model `%s` does not exist\",\"detail\":\"", modelID)
	const tail = "\"}}"
	if pad := size - len(head) - len(tail); pad > 0 {
		return head + strings.Repeat("a", pad) + tail
	}
	return head + tail
}

// TestProbeModel_OversizedRefusalPostpones pins the read cap as a real guard
// rather than a comment.
//
// io.ReadAll returns a NIL error when a LimitReader is exhausted, so a body cut
// off at goneProbeMaxBody arrives looking exactly like a body that ended: no
// error, and a prefix that still names the model beside a gone-phrase, because
// the classifier only ever reads the first 10 000 characters and the phrase is
// at the front. The model gets RETIRED on an answer the gateway never received
// in full — the one direction a probe is never allowed to take.
//
// The under-cap half is the control. Without it the test would also pass if the
// probe simply stopped retiring anything on a large body, or on any body at all.
func TestProbeModel_OversizedRefusalPostpones(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		size int
		want probeVerdict
	}{
		// Comfortably inside the cap: an ordinary refusal, read whole, retires.
		{"within the cap", goneProbeMaxBody - (8 << 10), probeRefused},
		// Past it: the same words, but what arrived is a prefix of an answer
		// whose real content nobody knows.
		{"past the cap", goneProbeMaxBody + (8 << 10), probeInconclusive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv, _ := probeServer(t, http.StatusNotFound, paddedRefusal("claude-sonnet-4", tc.size))
			h := newProbeHandler(t)
			if got := runProbe(t, h, probeCandidateFor(srv.URL, "claude-sonnet-4"), endpointTypeChat); got != tc.want {
				t.Fatalf("verdict = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestProbeModel_DialectAnswerIsBounded pins that the cap covers the reads this
// file does not make.
//
// goneProbeMaxBody is applied to resp.Body once, in probeModel, and that is the
// only reason it binds at all: remapMiniMaxBusinessError and both dialect
// translators live in other files and read the body WHOLE, with io.ReadAll and
// no budget of their own. Capping at the two judgement functions instead would
// have left every MiniMax, vertex-express and OpenAI-Responses candidate
// unbounded — and this all happens on the detached disable goroutine, with no
// client backpressure behind it, so a provider (or something impersonating one)
// answering 200 with a multi-gigabyte body would be read into memory in full
// with nothing to notice.
//
// The assertion is on the bytes the provider managed to send, because that is
// the only observable: the verdict is inconclusive either way, since a truncated
// Gemini answer does not parse and neither does a complete one made of filler.
func TestProbeModel_DialectAnswerIsBounded(t *testing.T) {
	t.Parallel()

	// Eight times the cap. The probe reads at most the cap plus its own drain of
	// the same size, so the margin is wide enough that neither socket buffering
	// nor the drain can be mistaken for the translator having read it all.
	const flood = 8 * goneProbeMaxBody
	var written atomic.Int64
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(done)
		w.Header().Set("Content-Type", "application/json")
		// Bounded so the handler always terminates: once the probe closes the
		// body these writes fail, and if they somehow do not, the loop still ends
		// rather than hanging the test's server shutdown.
		chunk := bytes.Repeat([]byte("a"), 32<<10)
		for written.Load() < flood {
			n, err := w.Write(chunk)
			written.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	// A vertex-express candidate, so the answer goes through
	// translateEgressResponseBody rather than through this file's own reads.
	h := &Handler{upstreamTransport: dialToTestServer(t, srv)}
	candidate := probeCandidateFor("http://us-central1-aiplatform.googleapis.com/v1", "gemini-2.0-flash")

	if got := runProbe(t, h, candidate, endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}
	select {
	case <-done:
	case <-time.After(goneProbeTimeout):
		t.Fatal("the provider never stopped writing, so the probe never stopped reading")
	}
	if n := written.Load(); n >= flood {
		t.Fatalf("the provider sent all %d bytes, so nothing bounded the read; the cap is %d", n, goneProbeMaxBody)
	}
}

// probeModelIDSeq numbers the model ids used in the metric assertions below, so
// each invocation of a test gets series of its own on the process-wide registry.
//
// Deliberately NOT a uuid, and the reason is a production behaviour worth
// knowing about: judgeProbeFailure classifies the body through
// util.SanitizeLogBody, which redacts anything uuid-shaped. A uuid-suffixed
// model id is therefore erased from the refusal that names it, the classifier
// finds nothing to attribute the gone-phrase to, and a refused probe reads as
// inconclusive. That is real behaviour for any provider whose model ids look
// like uuids; here it would just make the test lie.
var probeModelIDSeq atomic.Uint64

func uniqueProbeModelID(prefix string) string {
	return prefix + "-" + strconv.FormatUint(probeModelIDSeq.Add(1), 10)
}

// scrapeMetrics renders the process's Prometheus exposition through the real
// handler, which is the only read path the metrics package offers and the same
// one an operator's scrape takes.
func scrapeMetrics(t *testing.T) string {
	t.Helper()
	rr := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics scrape returned status %d", rr.Code)
	}
	return rr.Body.String()
}

// TestProbeForRetirement_RecordsItsVerdict pins that a probe accounts for
// itself. The counter is the only place an operator can see a probe that did
// NOT retire anything: a served verdict means the classifier nominated a live
// model, and neither that nor an inconclusive run leaves any trace but a log
// line.
//
// It drives probeForRetirement rather than probeModel because that is where the
// recording lives and where production enters, and it reads the count back
// through the real exposition handler rather than the counter variable, so the
// series name and its labels are pinned as the public contract they are.
//
// The exact count of 1 is the assertion worth having — it is what would catch a
// probe recorded twice — and holding it requires a model id that is unique per
// INVOCATION, not merely per case. The registry is process-wide and counters
// only ever accumulate, so a fixed id pins a value that is right on the first
// run and wrong on every one after it: `go test -count=2` failed all three
// subtests before probeModelIDSeq existed.
func TestProbeForRetirement_RecordsItsVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		// Taken as a function because the refusal has to name the very model
		// the probe asked for, and that id is not known until the subtest
		// builds its own.
		body func(modelID string) string
		want probeVerdict
	}{
		{
			name:   "refused",
			status: http.StatusNotFound,
			body:   goneRefusalBody,
			want:   probeRefused,
		},
		{
			name:   "served",
			status: http.StatusOK,
			body: func(string) string {
				return `{"choices":[{"message":{"role":"assistant","content":"Hi"}}]}`
			},
			want: probeServed,
		},
		{
			// A 500 is a provider fault rather than a statement about the
			// model, so it establishes nothing and must be counted as such.
			name:   "inconclusive",
			status: http.StatusInternalServerError,
			body: func(string) string {
				return `{"error":{"message":"internal error"}}`
			},
			want: probeInconclusive,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			modelID := uniqueProbeModelID("verdict-metric-" + tc.name)
			srv, _ := probeServer(t, tc.status, tc.body(modelID))
			h := newProbeHandler(t)
			candidate := probeCandidateFor(srv.URL, modelID)

			if got := h.probeForRetirement(candidate, endpointTypeChat); got != tc.want {
				t.Fatalf("verdict = %s, want %s", got, tc.want)
			}

			want := fmt.Sprintf(
				`modelhotel_retirement_probes_total{model=%q,provider="Probe Provider",verdict=%q} 1`,
				modelID, tc.want.String(),
			)
			if out := scrapeMetrics(t); !strings.Contains(out, want) {
				t.Errorf("metrics scrape missing %q", want)
			}
		})
	}
}

// TestProbeForRetirement_UnprobeableCandidateRecordsNothing guards the seam's
// one deliberate omission: the nil-candidate return has no provider or model to
// label, so it must not manufacture an "unknown" series. Every probe the
// gateway actually spends a request on has both.
func TestProbeForRetirement_UnprobeableCandidateRecordsNothing(t *testing.T) {
	t.Parallel()

	h := newProbeHandler(t)
	if got := h.probeForRetirement(modelCandidate{}, endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive", got)
	}

	// Scoped to this metric's own lines. Other counters run their empty labels
	// through the same labelOrUnknown fallback, so an unqualified search for
	// provider="unknown" would answer for the whole exposition.
	//
	// Still a claim about the whole PROCESS, not just this call: the registry is
	// shared, so a future test that probes with a blank provider name would fail
	// HERE rather than where it was written (and, being parallel, would do so
	// only when it happened to record before this scrape). Nothing in the package
	// does that today. If one ever needs to, give it its own registry rather than
	// loosening this.
	for _, line := range strings.Split(scrapeMetrics(t), "\n") {
		if !strings.HasPrefix(line, "modelhotel_retirement_probes_total{") {
			continue
		}
		if strings.Contains(line, `provider="unknown"`) || strings.Contains(line, `model="unknown"`) {
			t.Errorf("a candidate with nothing to label recorded a series: %s", line)
		}
	}
}

// TestJudgeProbeSuccess_TranslatesResponsesDialect pins the probe's dialect
// translation for the OpenAI Responses shape.
//
// The re-route cannot fire for the probe as its body stands today — it also
// requires tools in the request — so this is guarding a future change rather
// than current traffic, which is exactly why it is worth pinning. The flag is
// set by buildCandidateRequest, not by anything in the probe, so if the
// re-route rules ever widen to admit a probe body, an untranslated Responses
// object reads as a chat completion with no choices: probeDeliveredContent
// returns false, the verdict is inconclusive, and every retirement behind that
// provider is postponed forever with nothing in the logs to say why.
func TestJudgeProbeSuccess_TranslatesResponsesDialect(t *testing.T) {
	t.Parallel()

	const body = `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`

	candidate := probeCandidateFor("http://provider.invalid", "gpt-5.6-sol")
	st := newProbeState(candidate, endpointTypeChat, probeChatEndpoint)
	st.responsesAttempt = true

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	if got := judgeProbeSuccess(resp, st, candidate, endpointTypeChat); got != probeServed {
		t.Fatalf("verdict = %s, want served: a translated Responses answer carries content", got)
	}

	// The same body without the translation is the failure this guards against,
	// and it must not read as an answer.
	plain := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	untranslated := newProbeState(candidate, endpointTypeChat, probeChatEndpoint)
	if got := judgeProbeSuccess(plain, untranslated, candidate, endpointTypeChat); got != probeInconclusive {
		t.Fatalf("verdict = %s, want inconclusive for an untranslated Responses object", got)
	}
}
