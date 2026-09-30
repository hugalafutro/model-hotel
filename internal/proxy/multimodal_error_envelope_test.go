package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// lmStudioUnknownRoute answers the way LM Studio answers every route it does
// not serve: HTTP 200 and an error envelope.
func lmStudioUnknownRoute(calls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":"Unexpected endpoint or method. (POST `+r.URL.Path+`)"}`)
	})
}

// waitForRequestLog polls for the provider's newest row to reach a terminal
// state, since the terminal write is fire-and-forget.
func waitForRequestLog(t *testing.T, providerID uuid.UUID) (state string, status int, kind, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var k, m *string
		err := testDB.Pool().QueryRow(context.Background(),
			`SELECT state, status_code, error_kind, error_message FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`,
			providerID).Scan(&state, &status, &k, &m)
		if err == nil && state != "pending" && state != "streaming" {
			if k != nil {
				kind = *k
			}
			if m != nil {
				msg = *m
			}
			return state, status, kind, msg
		}
		if time.Now().After(deadline) {
			t.Fatalf("row never settled: state=%q err=%v", state, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A 2xx whose body is only an error envelope is the provider refusing, not
// answering. On the last candidate the client gets a 502 it can read as an
// error, and the row records a failure with the provider's own message, on
// every JSON-answering family and on speech, whose real answer is binary and
// whose JSON body can therefore only be the refusal.
func TestPassthrough_ErrorEnvelopeInside200IsAFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		body  string
		serve func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{"embeddings", "/v1/embeddings", `{"model":"%s","input":"hi"}`, (*Handler).Embeddings},
		{"rerank", "/v1/rerank", `{"model":"%s","query":"q","documents":["a"]}`, (*Handler).Rerank},
		{"images", "/v1/images/generations", `{"model":"%s","prompt":"a cat"}`, (*Handler).ImageGenerations},
		{"speech", "/v1/audio/speech", `{"model":"%s","input":"hi","voice":"alloy"}`, (*Handler).AudioSpeech},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMultimodalEnv(t, lmStudioUnknownRoute(nil))
			body := fmt.Sprintf(tc.body, env.providerName+"/"+env.modelName)
			w := httptest.NewRecorder()
			tc.serve(env.handler, w, env.request(tc.path, "application/json", strings.NewReader(body)))

			if w.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"error"`) || strings.Contains(w.Body.String(), "Unexpected endpoint") {
				t.Errorf("client body = %s, want the gateway's envelope without the provider's text", w.Body.String())
			}
			state, status, kind, msg := waitForRequestLog(t, env.providerID)
			if state != "failed" || status != http.StatusOK || kind != string(KindProviderError) {
				t.Errorf("row = %s/%d/%s, want failed/200/%s", state, status, kind, KindProviderError)
			}
			if !strings.Contains(msg, "Unexpected endpoint") {
				t.Errorf("row error = %q, want the provider's message", msg)
			}
		})
	}
}

// While a sibling remains, the refusal fails over and the sibling's answer is
// served.
func TestPassthrough_ErrorEnvelopeInside200FailsOver(t *testing.T) {
	var badCalls, goodCalls atomic.Int32
	envBad := newMultimodalEnv(t, lmStudioUnknownRoute(&badCalls))
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}]}`)
	}))
	t.Cleanup(good.Close)
	_, _, goodModelUUID, _ := createMultimodalProvider(t, good.URL)

	group := envBad.modelName
	if _, err := failover.NewRepository(testDB.Pool()).UpsertWithConfig(context.Background(), group,
		[]uuid.UUID{envBad.modelUUID, goodModelUUID},
		map[string]bool{envBad.modelUUID.String(): true, goodModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("create failover group: %v", err)
	}

	w := httptest.NewRecorder()
	envBad.handler.Embeddings(w, envBad.request("/v1/embeddings", "application/json",
		strings.NewReader(fmt.Sprintf(`{"model":"hotel/%s","input":"hi"}`, group))))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"embedding"`) {
		t.Fatalf("status = %d body = %s, want the sibling's answer", w.Code, w.Body.String())
	}
	if badCalls.Load() != 1 || goodCalls.Load() != 1 {
		t.Errorf("calls bad=%d good=%d, want 1 each", badCalls.Load(), goodCalls.Load())
	}
}

// An answer that carries content is served even with an error member beside
// it: only a body that delivered nothing is the provider refusing.
func TestPassthrough_ErrorMemberBesideContentIsServed(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"error":"partial"}`)
	}))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, env.request("/v1/embeddings", "application/json",
		strings.NewReader(fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName))))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want the answer served", w.Code, w.Body.String())
	}
	if state, _, _, _ := waitForRequestLog(t, env.providerID); state != "completed" {
		t.Errorf("row state = %q, want completed", state)
	}
}

// An envelope is an error member that carries something, with nothing else
// carrying anything but the envelope's own metadata. Any other member that
// carries something is content, whatever the family calls it.
func TestPassthroughErrorEnvelope_Shapes(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		isErr  bool
	}{
		"LM Studio unknown route":          {200, `{"error":"Unexpected endpoint or method."}`, true},
		"OpenAI-shaped envelope":           {200, `{"object":"error","error":{"message":"no","type":"x","code":"y"}}`, true},
		"empty list beside the error":      {200, `{"data":[],"error":"x"}`, true},
		"embeddings beside advisory error": {200, `{"data":[{"embedding":[0.1]}],"error":"partial"}`, false},
		"rerank beside advisory error":     {200, `{"results":[{"index":0}],"error":"partial"}`, false},
		"transcription beside error":       {200, `{"text":"hello","error":"advisory"}`, false},
		"a key of the provider's own":      {200, `{"images":["aGk="],"error":"x"}`, false},
		"no-error stamp":                   {200, `{"data":[],"error":null}`, false},
		"usage stamped on a refusal":       {200, `{"error":"x","usage":{"prompt_tokens":0}}`, true},
		"model echoed on a refusal":        {200, `{"error":"x","model":"gpt-4","system_fingerprint":"fp"}`, true},
		"empty object beside the error":    {200, `{"error":"x","data":{}}`, true},
		"padded empty object":              {200, `{"error":"x","data":{ }}`, true},
		"object content beside the error":  {200, `{"error":"x","data":{"n":1}}`, false},
		"other echoes on a refusal":        {200, `{"error":"x","provider":"p","service_tier":"t","version":"1","timestamp":1}`, true},
		"no error member at all":           {200, `{"data":[]}`, false},
		"not JSON":                         {200, `<html>oops</html>`, false},
		"non-2xx is not this path":         {500, `{"error":"x"}`, false},
	} {
		if _, got := passthroughErrorEnvelope(tc.status, []byte(tc.body)); got != tc.isErr {
			t.Errorf("%s: isErr = %v, want %v", name, got, tc.isErr)
		}
	}
	for body, want := range map[string]string{
		`{"error":{"message":"model unloaded"}}`:               "model unloaded",
		`{"error":"Unexpected endpoint or method. (POST /x)"}`: "Unexpected endpoint or method. (POST /x)",
	} {
		if msg, _ := passthroughErrorEnvelope(200, []byte(body)); msg != want {
			t.Errorf("%s: message = %q, want the provider's own text", body, msg)
		}
	}
}
