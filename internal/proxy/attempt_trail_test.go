package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// The unit contract of the trail: what a record may carry and when one is
// appended. The flow tests below drive the real attempt paths.

func TestAttemptDetail_MasksCapsAndCollapses(t *testing.T) {
	masker := newCredentialMasker("sk-live-secret-12345")
	body := "  provider said:\n  key sk-live-secret-12345 rejected   " + strings.Repeat("x", 300)
	got := attemptDetail(masker, body)
	if strings.Contains(got, "sk-live-secret-12345") {
		t.Fatalf("detail leaked the credential: %q", got)
	}
	if !strings.HasPrefix(got, "provider said: key [redacted] rejected ") {
		t.Errorf("detail = %q, want whitespace collapsed and the key masked", got)
	}
	if n := len([]rune(got)); n != maxAttemptDetailRunes+1 {
		t.Errorf("detail is %d runes, want %d plus the ellipsis", n, maxAttemptDetailRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a cut detail must end in an ellipsis: %q", got)
	}
	if attemptDetail(masker, "") != "" {
		t.Error("empty in, empty out")
	}
	if got := attemptDetail(credentialMasker{}, "sk-abcdefghijklmnopqrstuvwxyz0123456789 in prose"); strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("a zero masker must still redact key-shaped tokens: %q", got)
	}
}

func TestAttemptRecord_Lifecycle(t *testing.T) {
	cand := modelCandidateForBreaker(uuid.New())
	l := &requestLogData{}

	// Nothing open: closing appends nothing, JSON is NULL.
	l.closeAttemptRecord(500, KindProviderError, "x", "", 0)
	if len(l.attempts) != 0 || l.attemptsJSON() != nil {
		t.Fatalf("a close with nothing open appended: %+v", l.attempts)
	}

	l.openAttemptRecord(0, cand, false, time.Now().Add(-40*time.Millisecond), true)
	l.noteBreaker(breakerCharge)
	l.closeAttemptRecord(503, KindProviderError, "HTTP 503 upstream down", "", 0)
	// A second close is a no-op: the terminal write after an explicit close
	// must not append the same attempt twice.
	l.closeAttemptRecord(502, KindProviderError, "again", "", 0)
	if len(l.attempts) != 1 {
		t.Fatalf("attempts = %+v, want exactly one record", l.attempts)
	}
	rec := l.attempts[0]
	if rec.Attempt != 0 || rec.ProviderID != cand.provider.ID.String() || rec.Provider != cand.provider.Name || rec.Model != candidateModelID(cand) {
		t.Errorf("identity = %+v", rec)
	}
	if rec.Status != 503 || rec.ErrorKind != string(KindProviderError) || rec.Detail != "HTTP 503 upstream down" || rec.Breaker != breakerCharge {
		t.Errorf("fate = %+v", rec)
	}
	if rec.DurationMs < 40 {
		t.Errorf("duration = %v, want measured from the attempt's start", rec.DurationMs)
	}

	// Breaker off: the record says so without any verdict site running.
	l.openAttemptRecord(1, cand, true, time.Now(), false)
	l.closeAttemptRecord(200, "", "", "", 123)
	if got := l.attempts[1]; got.Breaker != breakerDisabled || !got.Hedged || got.TTFTMs != 123 {
		t.Errorf("disabled/hedged record = %+v", got)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(l.attemptsJSON(), &decoded); err != nil {
		t.Fatalf("json: %v", err)
	}
	if _, present := decoded[1]["error_kind"]; present {
		t.Errorf("empty fields must be omitted, got %v", decoded[1])
	}
	if decoded[0]["status"] != float64(503) || decoded[1]["hedged"] != true {
		t.Errorf("json = %v", decoded)
	}

	// Nil receivers are inert: unit tests drive attempt paths without a log.
	var none *requestLogData
	none.openAttemptRecord(0, cand, false, time.Now(), true)
	none.noteBreaker(breakerCharge)
	none.closeAttemptRecord(0, "", "", "", 0)
	none.appendBreakerSkip(cand.provider.ID, "p", "m")
	none.appendAttemptRecord(attemptRecord{})
	if none.attemptsJSON() != nil {
		t.Error("nil log produced JSON")
	}

	// A hedged loser's detail prefers the classifier's body excerpt for a 429,
	// then the error's own text, then its detail.
	loser := hedgeResult{idx: 2, reqErr: reqError{Kind: KindProviderSaturated, Detail: "HTTP 429"}, rateLimit: rateLimitVerdict{detail: "concurrent_budget_exceeded", phrase: "concurrent_budget_exceeded"}, status: 429, breaker: breakerNoop}
	if rec := hedgeLoserRecord(loser, cand, time.Now()); rec.Detail != "concurrent_budget_exceeded" || rec.Phrase != "concurrent_budget_exceeded" || rec.Status != 429 || !rec.Hedged || rec.Attempt != 2 {
		t.Errorf("hedged 429 loser = %+v", rec)
	}
	if rec := hedgeLoserRecord(hedgeResult{reqErr: reqError{Kind: KindProviderError, Detail: "HTTP 503"}}, cand, time.Now()); rec.Detail != "HTTP 503" {
		t.Errorf("hedged loser without a body falls back to the error detail, got %+v", rec)
	}
}

// latestAttempts reads the trail the terminal write stored for the newest
// request_logs row matching the model, decoded.
func latestAttempts(t *testing.T, modelID string) []attemptRecord {
	t.Helper()
	var raw []byte
	err := testDB.Pool().QueryRow(context.Background(),
		`SELECT attempts FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, modelID).Scan(&raw)
	if err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if raw == nil {
		return nil
	}
	var out []attemptRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode attempts %s: %v", raw, err)
	}
	return out
}

// waitForTrail polls for the terminal write, which runs after the response is
// answered and may still be in flight when the recorder returns.
func waitForTrail(t *testing.T, modelID string, want int) []attemptRecord {
	t.Helper()
	return pollTrail(t, want, func() []attemptRecord { return latestAttempts(t, modelID) })
}

// waitForTrailByProvider is waitForTrail keyed on the terminal provider, for
// the single-provider env whose stored model id is the provider's own.
func waitForTrailByProvider(t *testing.T, providerID uuid.UUID, want int) []attemptRecord {
	t.Helper()
	return pollTrail(t, want, func() []attemptRecord {
		var raw []byte
		err := testDB.Pool().QueryRow(context.Background(),
			`SELECT attempts FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`, providerID).Scan(&raw)
		if err != nil || raw == nil {
			return nil
		}
		var out []attemptRecord
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode attempts %s: %v", raw, err)
		}
		return out
	})
}

func pollTrail(t *testing.T, want int, read func() []attemptRecord) []attemptRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := read()
		if len(got) >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The sequential chat chain, non-streaming and streaming: a 503 on the first
// candidate and a 200 on the second leave two records, the failed one charged
// and the served one credited, while the flat columns keep the terminal
// attempt exactly as before.
func TestAttemptTrail_SequentialChain(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-streaming", true: "streaming"}[stream], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.Host, "one-slot") {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"message":"upstream is down for maintenance"}}`)
					return
				}
				var reqBody map[string]any
				_ = json.NewDecoder(r.Body).Decode(&reqBody)
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, chatCompletionJSON(reqBody["model"].(string)))
			}))
			defer upstream.Close()
			env := buildReplayEnv(t, upstream)

			w := replayRequestStream(t, env, stream)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
			}
			modelID := "hotel/" + env.group
			got := waitForTrail(t, modelID, 2)
			if len(got) != 2 {
				t.Fatalf("attempts = %+v, want the failed and the served attempt", got)
			}
			first, second := got[0], got[1]
			if first.Attempt != 0 || first.ProviderID != env.p1ID.String() || first.Status != 503 || first.ErrorKind != string(KindProviderError) || first.Breaker != breakerCharge {
				t.Errorf("attempt 0 = %+v", first)
			}
			if !strings.Contains(first.Detail, "down for maintenance") {
				t.Errorf("attempt 0 detail = %q, want the provider's sentence", first.Detail)
			}
			if second.Attempt != 1 || second.Status != 200 || second.ErrorKind != "" || second.Breaker != breakerSuccess || second.Hedged {
				t.Errorf("attempt 1 = %+v", second)
			}
			if second.DurationMs <= 0 || first.DurationMs <= 0 {
				t.Errorf("durations = %v / %v, want measured", first.DurationMs, second.DurationMs)
			}
			var flatAttempt int
			var flatProvider uuid.UUID
			if err := testDB.Pool().QueryRow(context.Background(), `SELECT failover_attempt, provider_id FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, modelID).Scan(&flatAttempt, &flatProvider); err != nil {
				t.Fatalf("flat columns: %v", err)
			}
			if flatAttempt != 1 || flatProvider == env.p1ID {
				t.Errorf("flat columns = attempt %d provider %s, want the terminal attempt unchanged", flatAttempt, flatProvider)
			}
		})
	}
}

// replayRequestStream is replayRequest with the stream flag chosen.
func replayRequestStream(t *testing.T, env *replayEnv, stream bool) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "hi"}], "stream": ` + map[bool]string{false: "false", true: "true"}[stream] + `}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
	w := httptest.NewRecorder()
	env.h.ChatCompletions(w, req.WithContext(ctx))
	return w
}

// A candidate the breaker refuses at resolve time leads the trail as a skip,
// numbered -1 because it was never attempted, ahead of the one that served.
func TestAttemptTrail_BreakerSkipLeadsTheTrail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionJSON(reqBody["model"].(string)))
	}))
	defer upstream.Close()
	env := buildReplayEnv(t, upstream)
	env.h.circuitBreaker.RecordExhausted(env.p1ID, "one-slot", "shared-model", 429, 0)

	w := replayRequest(t, env)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	got := waitForTrail(t, "hotel/"+env.group, 2)
	if len(got) != 2 {
		t.Fatalf("attempts = %+v, want the skip and the serve", got)
	}
	if got[0].Attempt != -1 || got[0].ProviderID != env.p1ID.String() || got[0].Breaker != breakerSkipped || got[0].Status != 0 || got[0].Model != "shared-model" {
		t.Errorf("skip record = %+v", got[0])
	}
	if got[1].Attempt != 0 || got[1].Status != 200 {
		t.Errorf("served record = %+v", got[1])
	}
}

// The saturation retry is two attempts: the 429 the last candidate answered,
// classified with its phrase and left uncharged, then the retry that served.
func TestAttemptTrail_SaturationRetry(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(neuralwattSaturatedBody))
			return
		}
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionJSON(reqBody["model"].(string))))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)

	if w := chatRequest(t, env); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	got := waitForTrailByProvider(t, env.ProviderID, 2)
	if len(got) != 2 {
		t.Fatalf("attempts = %+v, want the saturated 429 and its retry", got)
	}
	if got[0].Status != 429 || got[0].ErrorKind != string(KindProviderSaturated) || got[0].Phrase != "concurrent_budget_exceeded" || got[0].Breaker != breakerNoop {
		t.Errorf("saturated record = %+v", got[0])
	}
	if !strings.Contains(got[0].Detail, "concurrent_budget_exceeded") {
		t.Errorf("saturated detail = %q, want the provider's code", got[0].Detail)
	}
	if got[1].Attempt != 1 || got[1].Status != 200 || got[1].Breaker != breakerSuccess {
		t.Errorf("retry record = %+v", got[1])
	}
}

// A hedged race: the loser is recorded from its result (hedged, its error),
// the winner is opened at its launch and closed by the stream's terminal
// write like a sequential attempt.
func TestAttemptTrail_HedgedRace(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 5 * time.Millisecond, reqErr: reqError{Kind: KindProviderError, Attempt: 0, Provider: "a", Detail: "HTTP 503"}},
		{delay: 5 * time.Millisecond, won: true},
	})
	st, logData := newHedgeState(time.Millisecond)
	logData.id = uuid.New().String()
	cands := []modelCandidate{
		{model: &model.Model{ID: uuid.New(), ModelID: "m-a"}, provider: &provider.Provider{ID: uuid.New(), Name: "a", BaseURL: "http://a.upstream.test"}},
		{model: &model.Model{ID: uuid.New(), ModelID: "m-b"}, provider: &provider.Provider{ID: uuid.New(), Name: "b", BaseURL: "http://b.upstream.test"}},
	}
	w := runHedge(context.Background(), h, hh, st, cands)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	got := logData.attempts
	if len(got) != 2 {
		t.Fatalf("attempts = %+v, want the loser and the winner", got)
	}
	if !got[0].Hedged || got[0].Attempt != 0 || got[0].Provider != "a" || got[0].ErrorKind != string(KindProviderError) || got[0].Detail != "HTTP 503" {
		t.Errorf("loser = %+v", got[0])
	}
	if !got[1].Hedged || got[1].Attempt != 1 || got[1].Provider != "b" || got[1].Model != "m-b" || got[1].Status != 200 {
		t.Errorf("winner = %+v", got[1])
	}
}

// The pass-through attempt records itself through the same terminal write,
// with the breaker credit the buffered read gave it.
func TestAttemptTrail_Passthrough(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}]}`)
	}))
	defer srv.Close()
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small", InputModalities: `["text"]`, OutputModalities: `["embedding"]`}
	cand := goneCandidateAt(m, "P", "http://p.example")
	logData := &requestLogData{id: uuid.New().String(), modelID: "text-embedding-3-small", endpointType: endpointTypeEmbeddings}
	st := &requestState{
		startTime: time.Now(), reqModel: "text-embedding-3-small",
		endpointPath:          "/embeddings",
		bodyBytes:             []byte(`{"model":"text-embedding-3-small","input":"hi"}`),
		failoverTimeout:       30 * time.Second,
		circuitBreakerEnabled: true,
		logData:               logData,
	}
	if out := h.attemptPassthroughCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, cand, 0, 1); out != outcomeServed {
		t.Fatalf("outcome = %v, want served", out)
	}
	if len(logData.attempts) != 1 {
		t.Fatalf("attempts = %+v, want one served record", logData.attempts)
	}
	if rec := logData.attempts[0]; rec.Status != 200 || rec.Breaker != breakerSuccess || rec.Provider != "P" || rec.Model != "text-embedding-3-small" {
		t.Errorf("record = %+v", rec)
	}
}

// A request that never reaches a candidate (an unknown model) leaves the
// column NULL, the same as a row an older binary wrote.
func TestAttemptTrail_NullWithoutACandidate(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	body := `{"model": "nope/does-not-exist", "messages": [{"role": "user", "content": "hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req.WithContext(ctx))
	if w.Code == http.StatusOK {
		t.Fatal("an unknown model served")
	}
	time.Sleep(100 * time.Millisecond)
	if got := latestAttempts(t, "nope/does-not-exist"); got != nil {
		t.Errorf("attempts = %+v, want NULL for a request that reached no candidate", got)
	}
}

// The staleness report: a phrase seen in a trail inside the horizon is fresh,
// one added inside the horizon is fresh on its date alone, and one older than
// the horizon with no trail naming it is stale.
func TestStalePhrases(t *testing.T) {
	saved := rateLimitPhrases
	rateLimitPhrases = []rateLimitPhrase{
		{phrase: "seen-recently", class: rateLimitSaturated, provider: "P1", observed: "2026-01-01"},
		{phrase: "added-recently", class: rateLimitSaturated, provider: "P2", observed: time.Now().Format("2006-01-02")},
		{phrase: "never-seen", class: rateLimitSaturated, provider: "P3", observed: "2026-01-01"},
	}
	t.Cleanup(func() { rateLimitPhrases = saved })

	now := time.Now()
	pool := testDB.Pool()
	seeded := uuid.New().String()
	if _, err := pool.Exec(context.Background(), `INSERT INTO request_logs (id, model_id, status_code, created_at, attempts) VALUES ($1, 'stale-test', 200, $2, $3::jsonb)`,
		seeded, now.Add(-24*time.Hour), `[{"attempt":0,"provider_id":"x","provider":"P1","model":"m","status":429,"phrase":"seen-recently","duration_ms":1}]`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM request_logs WHERE id = $1`, seeded) })

	stale, err := StalePhrases(context.Background(), pool, now)
	if err != nil {
		t.Fatalf("StalePhrases: %v", err)
	}
	if len(stale) != 1 || stale[0].Phrase != "never-seen" || stale[0].Provider != "P3" {
		t.Errorf("stale = %+v, want only the never-seen phrase", stale)
	}
	// The report itself runs without error either way.
	ReportStalePhrases(context.Background(), pool, now)
}

// The report's other two outcomes, and the loop that schedules it: a healthy
// table logs nothing above Debug, a failing query is reported rather than
// swallowed, and the loop runs the report on its tick until its context ends.
func TestStalePhrases_ReportPathsAndLoop(t *testing.T) {
	saved := rateLimitPhrases
	rateLimitPhrases = []rateLimitPhrase{
		{phrase: "fresh-entry", class: rateLimitSaturated, provider: "P", observed: time.Now().Format("2006-01-02")},
	}
	t.Cleanup(func() { rateLimitPhrases = saved })
	capt := captureProxyLogs(t)
	pool := testDB.Pool()
	now := time.Now()

	// Every entry inside the horizon by its own date: nothing to query, nothing
	// stale, a Debug line only.
	ReportStalePhrases(context.Background(), pool, now)
	if len(capt.find("rate-limit phrases: entries unmatched inside the horizon; the provider may have rewritten its error text")) > 0 {
		t.Error("a fresh table was reported stale")
	}

	// A query that cannot run (cancelled context) is reported, not swallowed.
	rateLimitPhrases = []rateLimitPhrase{{phrase: "old-entry", class: rateLimitSaturated, provider: "P", observed: "2026-01-01"}}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StalePhrases(cancelled, pool, now); err == nil {
		t.Error("StalePhrases on a cancelled context returned no error")
	}
	ReportStalePhrases(cancelled, pool, now)
	if len(capt.find("rate-limit phrases: staleness check failed")) == 0 {
		t.Error("the failed check was not logged")
	}

	// The loop: first tick fires the report, cancel stops it.
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		phraseStalenessLoop(ctx, pool, 5*time.Millisecond, time.Hour)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for len(capt.find("rate-limit phrases: entries unmatched inside the horizon; the provider may have rewritten its error text")) == 0 {
		select {
		case <-deadline:
			t.Fatal("the loop never ran the report")
		case <-time.After(5 * time.Millisecond):
		}
	}
	stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not stop on context cancel")
	}
}

// The deferred answer verdict fires exactly once: the terminal write takes
// it, and the attempt path's fallback finds nothing left to run.
func TestJudgeAnswerNow_ExactlyOnce(t *testing.T) {
	runs := 0
	l := &requestLogData{judgeAnswer: func() { runs++ }}
	judgeAnswerNow(l)
	judgeAnswerNow(l)
	if runs != 1 || l.judgeAnswer != nil {
		t.Errorf("runs = %d, judgeAnswer nil = %v; want one run and the hook cleared", runs, l.judgeAnswer == nil)
	}
	judgeAnswerNow(&requestLogData{}) // nothing deferred: a no-op
}

// The terminal attempt keeps what the UPSTREAM said: a 429 that ends the
// request records the phrase it matched (the staleness report counts it), and
// a stream that died after its 200 headers records 200, not the 0 the client
// was answered.
func TestAttemptTrail_TerminalAttemptKeepsUpstreamFacts(t *testing.T) {
	t.Run("terminal exhausted 429 carries its phrase", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(ollamaExhaustedBody))
		}))
		defer upstream.Close()
		env := newTestProxyEnvWithUpstream(t, upstream)
		if w := chatRequest(t, env); w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
		}
		got := waitForTrailByProvider(t, env.ProviderID, 1)
		if len(got) != 1 || got[0].Status != 429 || got[0].Phrase != "session usage limit" || got[0].ErrorKind != string(KindProviderQuotaExhausted) || got[0].Breaker != breakerCharge {
			t.Errorf("terminal 429 record = %+v, want status 429 with its phrase and a charge", got)
		}
	})

	t.Run("a stream that dies after 200 headers records 200", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"error\":{\"message\":\"backend exploded mid-stream\"}}\n\n")
		}))
		defer upstream.Close()
		env := newTestProxyEnvWithUpstream(t, upstream)
		body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": true}`
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req.WithContext(ctx))

		got := waitForTrailByProvider(t, env.ProviderID, 1)
		if len(got) != 1 {
			t.Fatalf("attempts = %+v, want one", got)
		}
		var state string
		if err := testDB.Pool().QueryRow(context.Background(), `SELECT state FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`, env.ProviderID).Scan(&state); err != nil {
			t.Fatalf("state: %v", err)
		}
		if state != "failed" {
			t.Fatalf("row state = %q, want the in-stream error to fail the request", state)
		}
		if got[0].Status != 200 {
			t.Errorf("terminal record = %+v, want the upstream's 200 kept although the row's status is 0", got[0])
		}
	})
}
