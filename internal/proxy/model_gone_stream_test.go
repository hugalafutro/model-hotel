package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// Retirement tests about what a finished STREAM says: the verdict for each
// outcome, and how a served or empty completion moves the streak.

// TestNoteModelServed_ResetsStreak pins that the streak must be consecutive. A
// provider blip that happens to match a gone-pattern must not accumulate
// towards a disable across an otherwise healthy period.
func TestNoteModelServed_ResetsStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "glm-5.2"}
	cand := goneCandidateFor(t, m, "OpenCode Zen")

	for range goneStrikeThreshold * 3 {
		// One short of the threshold, then a success, forever.
		for i := 1; i < goneStrikeThreshold; i++ {
			h.noteModelGone(cand, endpointTypeChat)
		}
		h.noteModelServed(m, endpointTypeChat)
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a model that keeps answering must never be disabled, got %d calls", len(calls))
	}
}

// TestVerdictForStream pins the three-way rule that decides what a finished
// stream proves about a model. Review caught this twice: first that a
// gone-report mid-stream was never recorded at all, then that treating every
// non-gone outcome as a success let a retired model reset its own strike streak
// with its own failures and stay routable indefinitely.
func TestVerdictForStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     ErrorKind
		upstream ErrorKind
		produced bool
		want     streamVerdict
	}{
		{"clean finish that delivered content proves the model answered", "", "", true, verdictServed},
		{"explicit gone report strikes", KindProviderModelGone, KindProviderModelGone, true, verdictGone},
		{"gone report strikes even with no content", KindProviderModelGone, KindProviderModelGone, false, verdictGone},
		// The provider's verdict must survive a later cause overwriting the
		// recorded kind. A client hanging up on the error chunk is the ordinary
		// case, and judging the model by that would let the client suppress the
		// evidence by reacting to it.
		{"client hangup cannot erase the provider's gone report", KindClientDisconnect, KindProviderModelGone, false, verdictGone},
		{"nor can a stall reported after it", KindProviderTimeout, KindProviderModelGone, false, verdictGone},
		// A stream that opened, emitted nothing and ended without recording an
		// error is not proof of anything. Crediting it would clear a retirement
		// streak on the strength of an empty response.
		{"truncated stream with no content is inconclusive", "", "", false, verdictInconclusive},
		// Everything below is a failure that says nothing about whether the
		// model exists, so it must not clear the streak.
		{"transient provider error", KindProviderError, KindProviderError, true, verdictInconclusive},
		{"client hung up", KindClientDisconnect, "", true, verdictInconclusive},
		{"provider stalled", KindProviderTimeout, "", false, verdictInconclusive},
		{"failover deadline", KindFailoverTimeout, "", false, verdictInconclusive},
		{"retry deadline", KindRetryTimeout, "", false, verdictInconclusive},
		{"payload rejected", KindProviderBadRequest, KindProviderBadRequest, false, verdictInconclusive},
		{"not entitled", KindProviderNotEntitled, KindProviderNotEntitled, false, verdictInconclusive},
		{"gateway fault", KindInternal, "", false, verdictInconclusive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := verdictForStream(tc.kind, tc.upstream, tc.produced); got != tc.want {
				t.Errorf("verdictForStream(%q, upstream=%q, produced=%v) = %v, want %v", tc.kind, tc.upstream, tc.produced, got, tc.want)
			}
		})
	}
}

// TestNoteStreamOutcome_EmptyStreamDoesNotClearStreak is the composed form of
// the truncation case: a model accumulating strikes must not have them wiped by
// a stream that delivered nothing.
func TestNoteStreamOutcome_EmptyStreamDoesNotClearStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
		// An empty, error-free stream lands between strikes. The log entry
		// carries its endpoint family exactly as ingest stamps it, since that is
		// what noteStreamOutcome forwards on a gone verdict.
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat}, cand)
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the model to still be disabled, got %d disable calls", len(calls))
	}
}

// TestNoteStreamOutcome_RealSuccessClears is the other direction: a stream that
// genuinely delivered content still resets the streak.
func TestNoteStreamOutcome_RealSuccessClears(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "glm-5.2"}
	cand := goneCandidateFor(t, m, "OpenCode Zen")

	for range goneStrikeThreshold * 3 {
		for i := 1; i < goneStrikeThreshold; i++ {
			h.noteModelGone(cand, endpointTypeChat)
		}
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat, tokensCompletion: 5, ttftMs: 30}, cand)
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a model that keeps streaming content must never be disabled, got %d", len(calls))
	}
}

// TestNoteModelGone_FailedStreamsDoNotResetStreak is the composed consequence:
// a model that is genuinely gone still reaches the threshold even when its
// attempts are interleaved with unrelated stream failures, because those are
// inconclusive and touch neither counter.
func TestNoteModelGone_FailedStreamsDoNotResetStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
		// A transient stream failure lands between strikes. Under an "anything
		// not gone is a success" rule this would clear the streak and the model
		// could never be retired.
		if v := verdictForStream(KindProviderError, KindProviderError, true); v == verdictServed {
			h.noteModelServed(m, endpointTypeChat)
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the model to still be disabled, got %d disable calls", len(calls))
	}
}

// TestNoteStreamOutcome_InconclusiveTouchesNeitherCounter pins the middle
// verdict end to end, through the shared entry point both dispatch paths use.
//
// The verdict table is tested directly elsewhere, but that does not prove
// noteStreamOutcome acts on it: a stream that failed for an unrelated reason
// must leave the streak exactly where it was. Clearing it there lets a retired
// model stay routable forever on the strength of its own unrelated failures,
// and striking there retires a healthy model during an outage.
func TestNoteStreamOutcome_InconclusiveTouchesNeitherCounter(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// One short of the threshold, so a stray strike would disable and a stray
	// clear would be visible as a restart.
	for range goneStrikeThreshold - 1 {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// A timeout, a client disconnect and a transient provider failure: none of
	// them is evidence about whether the model still exists.
	for _, kind := range []ErrorKind{KindProviderTimeout, KindClientDisconnect, KindProviderError} {
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat, errorKind: kind}, cand)
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("an inconclusive stream must not disable anything, got %+v", calls)
	}

	// The streak survived intact, so the next real refusal completes it.
	h.noteModelGone(cand, endpointTypeChat)
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("an inconclusive stream cleared the streak: expected the disable, got %+v", calls)
	}
}

// TestAttemptCandidate_AnEmptyCompletionKeepsTheStreak pins that the
// non-streaming chat path judges the answer rather than the status.
//
// `200 {"choices":[]}` decodes, is forwarded to the client as a normal
// completion, and is exactly what an aggregator in front of a retired model can
// return between its gone-shaped 404s. Crediting it resets the count, so the
// three refusals never land consecutively and the model is never nominated,
// probed or retired. Every other path on this branch already draws this line.
func TestAttemptCandidate_AnEmptyCompletionKeepsTheStreak(t *testing.T) {
	cases := []struct {
		name       string
		answer     string
		wantStreak int64
	}{
		{"empty completion", `{"id":"x","object":"chat.completion","choices":[]}`, 1},
		// The control, without which this test would also pass against a path
		// that had simply stopped clearing streaks altogether.
		{"real completion", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],"usage":{"completion_tokens":1}}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandler(h)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.answer)
			}))
			defer srv.Close()

			m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol", InputModalities: `["text"]`, OutputModalities: `["text"]`}
			cand := goneCandidateAt(m, "OpenAI", srv.URL)

			// One real refusal, so there is a streak for the 200 to clear.
			h.noteModelGone(cand, endpointTypeChat)
			streak := goneStreakFor(t, h, m.ID, probeChatEndpoint)

			st := &requestState{
				startTime:       time.Now(),
				reqModel:        "gpt-5.6-sol",
				bodyBytes:       []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}`),
				failoverTimeout: 30 * time.Second,
				logData:         &requestLogData{modelID: "gpt-5.6-sol", endpointType: endpointTypeChat},
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

			if got := h.attemptCandidate(w, r, st, cand, 0, 1); got != outcomeServed {
				t.Fatalf("outcome = %v, want served", got)
			}
			if n := streak.count(); n != tc.wantStreak {
				t.Fatalf("streak = %d, want %d", n, tc.wantStreak)
			}
		})
	}
}

// TestNoteStreamOutcome_NilLogDataIsIgnored pins a guard that has to live at the
// dereference. producedOutput checks for nil as well, but noteStreamOutcome
// builds its arguments from logData in the same expression that calls it, and Go
// evaluates all of them first — so on this path the helper's check could never
// run and a nil would panic before reaching it.
func TestNoteStreamOutcome_NilLogDataIsIgnored(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}

	h.noteStreamOutcome(nil, goneCandidateFor(t, m, "Google AI Studio (Gemini)"))

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a stream with no log entry establishes nothing, got %+v", calls)
	}
}

// TestNoteModelServed_ClearsOnlyItsOwnSurface pins the other side of the split.
//
// A streak is about one surface because a model can be served on one and refused
// on another. Clearing every surface on any success answered both questions as
// one, and the direction it failed in is the one that matters: a provider that
// has retired a model's chat surface while still serving its embeddings would
// have every embeddings success wipe the chat streak, so the dead surface could
// never reach three consecutive strikes and would never be adjudicated at all.
//
// Narrowing it cannot retire anything wrongly, which is why it is safe to do:
// the surviving streak still has to be refused by a real PROBE to that same
// surface before a disable is written.
func TestNoteModelServed_ClearsOnlyItsOwnSurface(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	// A chat model. Nothing filters a request by modality on the way in, so it
	// can still be sent to /v1/embeddings and can still be served there — the
	// refusals that arrive on that surface are ignored, but a SUCCESS on it is
	// what this test is about.
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol", InputModalities: `["text"]`, OutputModalities: `["text"]`}
	cand := goneCandidateFor(t, m, "OpenAI")

	h.noteModelGone(cand, endpointTypeChat)

	// An embeddings request to the same model succeeds. It says nothing about
	// the chat surface, which is the one accused.
	h.noteModelServed(m, endpointTypeEmbeddings)
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 1 {
		t.Errorf("chat streak = %d, want the strike kept: an embeddings success is not evidence about chat", n)
	}

	// Nor does traffic on a surface that is never auto-retired.
	h.noteModelServed(m, endpointTypeImage)
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 1 {
		t.Errorf("chat streak = %d, want the strike kept after an image success", n)
	}

	// And the mirror: /v1/messages resolves to the chat surface, so a success
	// there clears the streak a /v1/chat/completions refusal built.
	h.noteModelServed(m, endpointTypeMessages)
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 0 {
		t.Errorf("chat streak = %d, want 0 after a success on the same surface", n)
	}
}

// TestNoteModelServed_AnUnprobeableFamilyClearsNothing pins the rule the strike
// side already follows, applied to the success side: a family that cannot be
// adjudicated does not get to speak about one that can. An image or TTS response
// says no more about the chat surface than an image refusal does, and crediting
// it would let traffic on an unprobeable surface hold a genuinely dead chat
// surface open indefinitely.
func TestNoteModelServed_AnUnprobeableFamilyClearsNothing(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.5-flash-image"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	h.noteModelGone(cand, endpointTypeChat)

	for _, family := range []string{endpointTypeImage, endpointTypeTTS, endpointTypeSTT, endpointTypeRerank, ""} {
		h.noteModelServed(m, family)
		if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 1 {
			t.Fatalf("a %q success cleared the chat streak (now %d)", family, n)
		}
	}
}
