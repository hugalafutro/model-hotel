package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// hangingUpstream accepts the connection and sends nothing until the gateway
// gives up: the pre-header stall.
func hangingUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// net/http only watches for the client going away once the request
		// body has been read, so the fixture drains it before it stalls.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

// A provider that never sends response headers is a stalled provider, whether
// this gateway's own request_timeout or the transport's header timeout ends
// the wait: the caller is still there, and the breaker is charged the way the
// streaming probe and the body readers already charge a stall. Only an
// abandoned attempt (the client hung up) is not charged.
func TestDoUpstream_PreHeaderStallIsChargedToTheProvider(t *testing.T) {
	t.Run("this gateway's request_timeout", func(t *testing.T) {
		env := newTestProxyEnvWithUpstream(t, hangingUpstream(t))
		withRequestTimeout(t, env.Handler, "100ms")

		w := chatRequest(t, env)
		if w.Code != http.StatusGatewayTimeout {
			t.Errorf("status = %d, want 504: %s", w.Code, w.Body.String())
		}
		if fails := breakerConsecutiveFails(env.Handler, env.ProviderID); fails != 1 {
			t.Errorf("breaker failures = %d, want 1: a pre-header stall must charge the provider", fails)
		}
	})
	t.Run("the transport's header timeout", func(t *testing.T) {
		env := newTestProxyEnvWithUpstream(t, hangingUpstream(t))
		ctx := context.Background()
		if err := env.Handler.settingsRepo.Set(ctx, "upstream_header_timeout", "100ms"); err != nil {
			t.Fatalf("set upstream_header_timeout: %v", err)
		}
		env.Handler.settingsRepo.InvalidateCache("upstream_header_timeout")
		t.Cleanup(func() {
			_ = env.Handler.settingsRepo.DeleteKey(ctx, "upstream_header_timeout")
			env.Handler.settingsRepo.InvalidateCache("upstream_header_timeout")
		})

		w := chatRequest(t, env)
		// provider_timeout, the same kind a stalled body read records: the
		// provider's failure (502), not this gateway's deadline (504).
		if w.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want 502: %s", w.Code, w.Body.String())
		}
		if fails := breakerConsecutiveFails(env.Handler, env.ProviderID); fails != 1 {
			t.Errorf("breaker failures = %d, want 1: the header timeout is the provider's stall", fails)
		}
	})
}

// One attempt ends at the overall request deadline when that comes first,
// so the last candidate cannot overrun the budget the loop only checks
// between candidates.
func TestAttemptDeadline_IsCutAtTheOverallDeadline(t *testing.T) {
	st := &requestState{failoverTimeout: 10 * time.Second}
	if got := time.Until(st.attemptDeadline()); got < 9*time.Second || got > 10*time.Second {
		t.Errorf("no overall deadline: attempt ends in %v, want a full failover timeout", got)
	}
	st.overallDeadline = time.Now().Add(time.Second)
	if got := time.Until(st.attemptDeadline()); got > time.Second {
		t.Errorf("overall deadline in 1s: attempt ends in %v, want at most 1s", got)
	}
}

// A TTFT probe that fails is not a consumed success: the in-flight slot the
// attempt holds settles unclean, so the provider's learned window does not
// grow on a stream that never delivered a token. The body is wrapped exactly
// as finishAttemptAdmission wraps it, clean from the 2xx, so the close alone
// would have credited the attempt. Two ways a probe fails: a read error the
// dispatch's own close settles, and the TTFT timeout, where the probe closes
// the body from its own goroutine before the dispatch gets to. The third case
// wraps the release the way the translated dialects do (the probe never sees
// the release itself), which is why the verdict lives on the slot.
func TestDispatchStreaming_ProbeFailureSettlesTheSlotUnclean(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	ctx := context.Background()
	if err := h.settingsRepo.Set(ctx, "ttft_timeout", "25ms"); err != nil {
		t.Fatalf("set ttft_timeout: %v", err)
	}
	h.settingsRepo.InvalidateCache("ttft_timeout")
	t.Cleanup(func() {
		_ = h.settingsRepo.DeleteKey(ctx, "ttft_timeout")
		h.settingsRepo.InvalidateCache("ttft_timeout")
	})

	for _, tc := range []struct {
		name string
		body io.ReadCloser
		wrap bool
	}{
		{"read error", io.NopCloser(iotest{}), false},
		{"TTFT timeout", newBlockUntilClosedReader(""), false},
		{"TTFT timeout behind a dialect adapter", newBlockUntilClosedReader(""), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settled := make(chan bool, 1)
			slot := &attemptSlot{fire: func(clean bool) { settled <- clean }}
			var body io.ReadCloser = &inflightRelease{ReadCloser: tc.body, slot: slot, clean: true, onEOF: true}
			if tc.wrap {
				body = struct{ io.ReadCloser }{body}
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: body}
			logData := streamingLog()
			logData.providerName = "probe-fail-provider"
			h.insertRequestLogAsync(logData)
			st := &requestState{
				startTime:             time.Now(),
				reqModel:              "test-model",
				isStreaming:           true,
				circuitBreakerEnabled: true,
				logData:               logData,
				attemptSlot:           slot,
			}
			cand := modelCandidate{
				model:    &model.Model{ModelID: "test-model"},
				provider: &provider.Provider{ID: uuid.New(), Name: "probe-fail-provider"},
				apiKey:   "sk-test",
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			if got := h.dispatchStreaming(w, req, st, cand, resp, 1, 10, "failover_timeout"); got != outcomeFailover {
				t.Fatalf("outcome = %v, want failover", got)
			}
			select {
			case clean := <-settled:
				if clean {
					t.Error("the slot settled clean on a probe that failed")
				}
			default:
				t.Fatal("the slot never settled")
			}
		})
	}
}

// dataErrReader returns the whole payload and io.EOF from one Read, the way
// testing/iotest.DataErrReader does (that name is taken in this package).
type dataErrReader struct{ r *strings.Reader }

func (d dataErrReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	if err == nil && d.r.Len() == 0 {
		err = io.EOF
	}
	return n, err
}

// The first token and the upstream's EOF can arrive in one read. The probe's
// hold keeps that EOF from settling the slot while the verdict is still
// "unclean until a token"; once the token is in, the stream's own end settles
// it clean, so a delivered one-read stream keeps its credit.
func TestDispatchStreaming_FirstTokenAndEOFInOneReadSettlesClean(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	settled := make(chan bool, 1)
	slot := &attemptSlot{fire: func(clean bool) { settled <- clean }}
	oneRead := dataErrReader{r: strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &inflightRelease{ReadCloser: io.NopCloser(oneRead), slot: slot, clean: true, onEOF: true},
	}
	logData := streamingLog()
	logData.providerName = "one-read-provider"
	h.insertRequestLogAsync(logData)
	st := &requestState{
		startTime:             time.Now(),
		reqModel:              "test-model",
		isStreaming:           true,
		circuitBreakerEnabled: true,
		logData:               logData,
		attemptSlot:           slot,
	}
	cand := modelCandidate{
		model:    &model.Model{ModelID: "test-model"},
		provider: &provider.Provider{ID: uuid.New(), Name: "one-read-provider"},
		apiKey:   "sk-test",
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	if got := h.dispatchStreaming(w, req, st, cand, resp, 1, 10, "failover_timeout"); got != outcomeServed {
		t.Fatalf("outcome = %v, want served: %s", got, w.Body.String())
	}
	select {
	case clean := <-settled:
		if !clean {
			t.Error("a delivered one-read stream settled unclean")
		}
	default:
		t.Fatal("the slot never settled")
	}
}

// [DONE] ends the stream: the body is closed rather than drained, so an
// upstream that lingers past its own sentinel does not hold the handler (and
// the caller's end of stream) until the stall watchdog fires.
func TestHandleStreamingResponse_DoneClosesTheBodyWithoutDraining(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	body := newBlockUntilClosedReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := streamingLog()
	logData.insertWg.Add(1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout", streamStallTimeout: 30 * time.Second})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler read on past [DONE] for the upstream's EOF")
	}
	select {
	case <-body.closed:
	default:
		t.Error("the upstream body was not closed")
	}
	if !strings.Contains(w.Body.String(), "[DONE]") {
		t.Errorf("the caller did not receive [DONE]: %q", w.Body.String())
	}
}

// A provider that splits its error object across data lines has those
// fragments dropped (they are not valid JSON), so the caller has not seen the
// error. It is still owed to them: the stream ends with the terminal frame
// carrying the reassembled message, not a bare close.
func TestHandleStreamingResponse_HeldErrorFragmentReachesTheCallerInTheTerminalFrame(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {\"error\":{\"message\":\"upstream boom\"\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := streamingLog()
	logData.insertWg.Add(1)

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed", logData.state)
	}
	frame := lastSSEError(t, w.Body.String())
	if frame == nil {
		t.Fatalf("no terminal error frame reached the caller: %q", w.Body.String())
	}
	if msg, _ := frame["message"].(string); !strings.Contains(msg, "upstream boom") {
		t.Errorf("terminal frame message = %q, want the held error", msg)
	}
}
