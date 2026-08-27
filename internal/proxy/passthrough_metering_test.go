package proxy

import (
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

// oversizedPassthroughBody returns a JSON body one byte past the buffer cap, the
// shape that sends serveBufferedJSONPassthrough down its streaming branch.
// Deliberately built at the real cap rather than against a lowered test
// constant: the branch is chosen by comparing against passthroughJSONBufferCap
// itself, so a test that shrinks the cap would not exercise the same decision.
func oversizedPassthroughBody() string {
	return `{"data":"` + strings.Repeat("a", passthroughJSONBufferCap) + `"}`
}

func passthroughCandidate() modelCandidate {
	return modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-x"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}
}

// TestPassthrough_OversizedJSONIsStillMetered is the metering hole itself. The
// oversized branch forwards a complete provider response to the client and then
// finalized the log with (0,0) tokens and never called recordTokenUsage, so the
// request was charged against nothing: not the key's tokens_used counter, not
// its TPM budget.
//
// This is not an exotic path. The buffer cap is 8 MiB and a batch embeddings
// call clears it at roughly 140 inputs of 3072 dimensions, which is ordinary
// document-indexing traffic, so the free requests are the routine ones.
func TestPassthrough_OversizedJSONIsStillMetered(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "text-embedding-x",
		endpointType:    endpointTypeEmbeddings,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 400,
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(oversizedPassthroughBody())),
	}
	rec := httptest.NewRecorder()
	h.serveBufferedJSONPassthrough(rec, st, passthroughCandidate(), resp, "application/json", 1, 10.0)

	// 400 prompt bytes at the conventional 4 bytes per token.
	const wantPrompt = 100
	if logData.tokensPrompt != wantPrompt {
		t.Errorf("logged prompt tokens = %d, want %d (estimated from promptTextBytes)", logData.tokensPrompt, wantPrompt)
	}
	if got := singleAddTokens(t, vkRepo); got != wantPrompt {
		t.Errorf("charged %d tokens against the key, want %d", got, wantPrompt)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestPassthrough_OversizedJSONDoesNotEstimateFromResponseBytes pins the half of
// the estimate that must NOT happen here. The streaming path estimates output
// from delivered bytes because those bytes are text, but a pass-through response
// is float vectors or base64 image data: charging 8 MiB of embedding floats at
// four bytes per token would invent roughly two million completion tokens.
// Undercharging the output is the deliberate choice; inventing it is not.
func TestPassthrough_OversizedJSONDoesNotEstimateFromResponseBytes(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = &mockVirtualKeyRepo{}

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "text-embedding-x",
		endpointType:    endpointTypeEmbeddings,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 400,
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(oversizedPassthroughBody())),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), st, passthroughCandidate(), resp, "application/json", 1, 10.0)

	if logData.tokensCompletion != 0 {
		t.Errorf("completion tokens = %d, want 0; response bytes are not text and must not be estimated as tokens", logData.tokensCompletion)
	}
}

// TestPassthrough_OversizedJSONWithNoPromptChargesNothing keeps the estimate
// honest in the other direction: with no prompt text recorded there is nothing
// to derive a charge from, and inventing one would bill a key for a request
// whose size the gateway never measured.
func TestPassthrough_OversizedJSONWithNoPromptChargesNothing(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "text-embedding-x",
		endpointType:    endpointTypeEmbeddings,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 0,
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(oversizedPassthroughBody())),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), st, passthroughCandidate(), resp, "application/json", 1, 10.0)

	if logData.tokensPrompt != 0 || logData.tokensCompletion != 0 {
		t.Errorf("usage = (%d,%d), want (0,0) when no prompt bytes were measured", logData.tokensPrompt, logData.tokensCompletion)
	}
	if len(vkRepo.addTokensCalls) != 0 {
		t.Errorf("charged the key %d times, want 0", len(vkRepo.addTokensCalls))
	}
}
