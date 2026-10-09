package proxy

import (
	"context"
	"errors"
	"io"
	"math"
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

// A streaming attempt that answered 2xx and was then left behind cost its
// provider's prompt. Before these charges, a hedged race metered only its
// winner and a sequential walk only the candidate that served: every abandoned
// stream's prompt was billed to the operator and charged to nobody.

func pricedModel(in, out float64) *model.Model {
	return &model.Model{ModelID: "priced", InputPricePerMillion: &in, OutputPricePerMillion: &out}
}

func nearly(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestRejectStreamPrompt_PricedButKeptOutOfTheColumns(t *testing.T) {
	served := func() *requestLogData {
		return &requestLogData{
			state:            "completed",
			promptTextBytes:  400, // 100 estimated tokens
			tokensPrompt:     10,
			tokensCompletion: 5,
			servedModel:      pricedModel(2, 4),
		}
	}
	loser := modelCandidate{model: pricedModel(1, 1), provider: &provider.Provider{Name: "loser"}}

	t.Run("priced loser", func(t *testing.T) {
		logData := served()
		if got := rejectStreamPrompt(logData, loser); got != 100 {
			t.Fatalf("charged %d tokens, want 100 (400 prompt bytes)", got)
		}
		if logData.tokensPrompt != 10 {
			t.Errorf("tokens_prompt = %d, want 10: the token columns carry only measured figures", logData.tokensPrompt)
		}
		if p, hit := logData.rejectedTokens(); p != 0 || hit != 0 {
			t.Errorf("rejectedTokens = %d/%d, want 0/0: an estimate was never in the columns", p, hit)
		}
		serving, rejected, ok := logData.terminalCostParts()
		if !ok || !nearly(serving, 40e-6) || !nearly(rejected, 100e-6) {
			t.Errorf("cost parts = %v/%v/%v, want 40e-6 serving (10x$2 + 5x$4 per M) and 100e-6 rejected (100x$1 per M)", serving, rejected, ok)
		}
	})

	t.Run("unpriced loser is priced with the serving share", func(t *testing.T) {
		logData := served()
		rejectStreamPrompt(logData, modelCandidate{model: &model.Model{ModelID: "free"}, provider: &provider.Provider{Name: "loser"}})
		serving, rejected, ok := logData.terminalCostParts()
		if !ok || !nearly(serving, 240e-6) || rejected != 0 {
			t.Errorf("cost parts = %v/%v/%v, want 240e-6 serving ((10+100)x$2 + 5x$4 per M) and nothing rejected", serving, rejected, ok)
		}
	})

	t.Run("a race with no winner still prices the charge", func(t *testing.T) {
		logData := &requestLogData{state: "failed", promptTextBytes: 400}
		rejectStreamPrompt(logData, loser)
		if logData.servedModel != loser.model {
			t.Fatal("servedModel not stamped: the terminal write prices nothing on a row without one")
		}
		if _, rejected, ok := logData.terminalCostParts(); !ok || !nearly(rejected, 100e-6) {
			t.Errorf("rejected cost = %v (ok %v), want 100e-6", rejected, ok)
		}
	})

	t.Run("a priced loser replaces an unpriced stamp", func(t *testing.T) {
		logData := &requestLogData{state: "failed", promptTextBytes: 400}
		rejectStreamPrompt(logData, modelCandidate{model: &model.Model{ModelID: "free"}, provider: &provider.Provider{Name: "free"}})
		rejectStreamPrompt(logData, loser)
		if logData.servedModel != loser.model {
			t.Fatal("the unpriced first loser kept servedModel, so the priced loser's charge prices to nothing")
		}
	})

	t.Run("an unpriced last candidate does not drop a priced charge", func(t *testing.T) {
		// The sequential walk stamps every candidate it dispatches: the priced
		// candidate's probe failed, then an unpriced one refused with a 503.
		logData := &requestLogData{state: "failed", promptTextBytes: 400}
		rejectStreamPrompt(logData, loser)
		logData.servedModel = &model.Model{ModelID: "free"}
		if serving, rejected, ok := logData.terminalCostParts(); !ok || serving != 0 || !nearly(rejected, 100e-6) {
			t.Errorf("cost parts = %v/%v/%v, want 0 serving and 100e-6 rejected", serving, rejected, ok)
		}
	})

	t.Run("no prompt text charges nothing", func(t *testing.T) {
		logData := &requestLogData{}
		if got := rejectStreamPrompt(logData, loser); got != 0 || len(logData.rejected) != 0 {
			t.Errorf("charged %d with %d records, want nothing", got, len(logData.rejected))
		}
	})
}

func TestStreamPromptBilled(t *testing.T) {
	if streamPromptBilled(&upstreamFrameError{msg: "overloaded"}) {
		t.Error("the provider's own error frame was billed")
	}
	for _, err := range []error{&emptyStreamError{}, errors.New("TTFT timeout"), errors.New("TTFT probe read error")} {
		if !streamPromptBilled(err) {
			t.Errorf("%v was not billed", err)
		}
	}
}

// The sequential walk: a candidate whose probe fails behind a 2xx is charged
// before the walk moves on, unless the provider ended it with its own error.
func TestDispatchStreaming_AbandonedProbeChargesItsPrompt(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	for _, tc := range []struct {
		name, body string
		charged    bool
	}{
		{"empty stream", "data: [DONE]\n\n", true},
		{"provider error frame", `data: {"error":{"message":"overloaded"}}` + "\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logData := streamingLog()
			logData.id = ""
			logData.promptTextBytes = 40
			st := &requestState{startTime: time.Now(), reqModel: "test-model", isStreaming: true, logData: logData}
			cand := modelCandidate{
				model:    &model.Model{ModelID: "test-model"},
				provider: &provider.Provider{ID: uuid.New(), Name: "abandoned"},
				apiKey:   "sk-test",
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			if got := h.dispatchStreaming(httptest.NewRecorder(), req, st, cand, resp, 0, 10, "failover_timeout"); got != outcomeFailover {
				t.Fatalf("outcome = %v, want failover", got)
			}
			if !tc.charged {
				if len(logData.rejected) != 0 {
					t.Errorf("charged %+v for a request the provider reported failed", logData.rejected)
				}
				return
			}
			if len(logData.rejected) != 1 || logData.rejected[0].prompt != 10 || !logData.rejected[0].estimated || logData.rejected[0].providerName != "abandoned" {
				t.Errorf("rejected = %+v, want one estimated 10-token charge to the abandoned provider", logData.rejected)
			}
		})
	}
}

// The hedged race: a loser charges as it arrives, a launch still running when
// the winner is picked charges at that moment, and a candidate refused before
// any 2xx charges nothing.
func TestRunHedgedStreaming_ChargesEveryAbandonedStream(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	charges := func(logData *requestLogData) map[string]int {
		out := map[string]int{}
		for _, a := range logData.rejected {
			if !a.estimated {
				t.Errorf("charge %+v is not marked estimated", a)
			}
			out[a.providerName] += a.prompt
		}
		return out
	}

	t.Run("winner", func(t *testing.T) {
		providerErr := reqError{Kind: KindProviderError}
		hh := newHedgeHarness([]fakeProbeSpec{
			{delay: 2 * time.Second, billed: true},                       // still probing when the winner lands
			{delay: time.Millisecond, billed: true, reqErr: providerErr}, // lost behind a 2xx
			{delay: time.Millisecond, reqErr: providerErr},               // refused before any 2xx
			{delay: 30 * time.Millisecond, won: true},
		})
		st, logData := newHedgeState(5 * time.Millisecond)
		logData.promptTextBytes = 40
		w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B", "prov-C", "prov-D"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got := charges(logData)
		if len(got) != 2 || got["prov-A"] != 10 || got["prov-B"] != 10 {
			t.Errorf("charges = %v, want 10 tokens each for prov-A (cut in flight) and prov-B (lost), none for prov-C or the winner", got)
		}
	})

	t.Run("exhausted", func(t *testing.T) {
		providerErr := reqError{Kind: KindProviderError}
		hh := newHedgeHarness([]fakeProbeSpec{
			{delay: time.Millisecond, billed: true, reqErr: providerErr},
			{delay: time.Millisecond, billed: true, reqErr: providerErr},
		})
		st, logData := newHedgeState(5 * time.Millisecond)
		logData.promptTextBytes = 40
		runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))
		if got := charges(logData); len(got) != 2 || got["prov-A"] != 10 || got["prov-B"] != 10 {
			t.Errorf("charges = %v, want 10 tokens each for both losers", got)
		}
		if logData.servedModel == nil {
			t.Error("an exhausted race left servedModel nil, so its charges price to nothing")
		}
	})
}

// The real probe raises the flag the orchestrator charges from once the
// provider answers 2xx, and lowers it again for the provider's own error.
func TestProbeStreamingCandidate_RaisesTheBilledFlag(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	for _, tc := range []struct {
		name   string
		status int
		body   string
		billed bool
	}{
		{"empty 2xx stream", http.StatusOK, "data: [DONE]\n\n", true},
		{"provider error frame", http.StatusOK, `data: {"error":{"message":"overloaded"}}` + "\n\n", false},
		{"refused", http.StatusInternalServerError, "boom", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			st, cand := probeStateForServer(srv.URL)
			st.hedgeBilled = &atomic.Bool{}
			if res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second); res.won {
				t.Fatal("the probe won")
			}
			if got := st.hedgeBilled.Load(); got != tc.billed {
				t.Errorf("billed = %v, want %v", got, tc.billed)
			}
		})
	}
}
