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

// TestPassthroughPromptTextBytes_SizesRealBodies is the test the first attempt
// at this fix should have had. That attempt estimated the prompt from
// logData.promptTextBytes, which ingestRequest fills using the CHAT rule: it
// looks for "messages" and "tools", finds neither in any pass-through body, and
// returns zero. The estimate was therefore always zero and the fix charged
// nothing, while a unit test that hand-set promptTextBytes on a fabricated
// logData reported success.
//
// These are the real request shapes, so a regression back to the chat-only
// sizer fails here instead of passing.
func TestPassthroughPromptTextBytes_SizesRealBodies(t *testing.T) {
	for _, tc := range []struct {
		name         string
		endpointType string
		body         string
		want         int
	}{
		{
			name:         "embeddings single string",
			endpointType: endpointTypeEmbeddings,
			body:         `{"model":"text-embedding-3","input":"hello world"}`,
			want:         11,
		},
		{
			name:         "embeddings batch",
			endpointType: endpointTypeEmbeddings,
			body:         `{"model":"text-embedding-3","input":["abcd","efghij"]}`,
			want:         10,
		},
		{
			name:         "embeddings pre-tokenised",
			endpointType: endpointTypeEmbeddings,
			body:         `{"model":"text-embedding-3","input":[1,2,3]}`,
			want:         3 * bytesPerToken,
		},
		{
			name:         "embeddings pre-tokenised batches",
			endpointType: endpointTypeEmbeddings,
			body:         `{"model":"text-embedding-3","input":[[1,2],[3,4,5]]}`,
			want:         5 * bytesPerToken,
		},
		{
			name:         "rerank query and documents",
			endpointType: endpointTypeRerank,
			body:         `{"model":"rerank-1","query":"abc","documents":["de","fgh"]}`,
			want:         8,
		},
		{
			name:         "image prompt",
			endpointType: endpointTypeImage,
			body:         `{"model":"dall-e-3","prompt":"a red cube"}`,
			want:         10,
		},
		{
			name:         "tts input",
			endpointType: endpointTypeTTS,
			body:         `{"model":"tts-1","input":"say this","voice":"alloy"}`,
			want:         8,
		},
		{
			name:         "multipart family is not guessed at",
			endpointType: endpointTypeSTT,
			body:         `{"model":"whisper-1"}`,
			want:         0,
		},
		{
			name:         "unparseable body sizes as zero",
			endpointType: endpointTypeEmbeddings,
			body:         `not json`,
			want:         0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := passthroughPromptTextBytes([]byte(tc.body), tc.endpointType); got != tc.want {
				t.Errorf("passthroughPromptTextBytes = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestPassthroughPromptTextBytes_IgnoresBlobs keeps the sizer from doing the one
// thing that would be worse than undercharging. A base64 image or an audio blob
// is orders of magnitude larger than the tokens it costs, so sizing it by bytes
// would invent an enormous charge. promptTextBytes already refuses to measure
// image_url and input_audio parts for the same reason.
func TestPassthroughPromptTextBytes_IgnoresBlobs(t *testing.T) {
	blob := strings.Repeat("A", 4096) // stands in for a base64 image payload
	body := `{"model":"dall-e-3","prompt":"tiny","image":"` + blob + `"}`
	got := passthroughPromptTextBytes([]byte(body), endpointTypeImage)
	if got != len("tiny") {
		t.Errorf("sized = %d, want %d: only the prompt text may be measured", got, len("tiny"))
	}
}

// TestEstimateTokens_UsesTheDocumentedRatio pins the conversion the sizer feeds,
// so the pre-tokenised branch above stays in the same unit as the text branches.
func TestEstimateTokens_UsesTheDocumentedRatio(t *testing.T) {
	if got := estimateTokens(bytesPerToken * 3); got != 3 {
		t.Errorf("estimateTokens(%d) = %d, want 3", bytesPerToken*3, got)
	}
	if got := estimateTokens(1); got != 1 {
		t.Errorf("estimateTokens(1) = %d, want 1 (any delivered text costs at least one token)", got)
	}
	if got := estimateTokens(0); got != 0 {
		t.Errorf("estimateTokens(0) = %d, want 0", got)
	}
}

// TestPassthrough_OversizedJSONChargesTheEstimate is the end-to-end half: the
// oversized branch forwards a complete provider response, and must charge the
// caller's quota for it rather than nothing. The prompt size is derived from a
// real embeddings body through the same sizer serveJSONPassthrough uses, so the
// test cannot pass on a value production would never produce.
//
// The request LOG stays at zero on purpose. Usage was never extracted here, so
// nothing was measured, and the log reports measured usage only — the estimate
// charges the quota without being reported as a provider figure, exactly as the
// chat and streaming paths do it.
func TestPassthrough_OversizedJSONChargesTheEstimate(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	reqBody := `{"model":"text-embedding-3","input":"` + strings.Repeat("d", 400) + `"}`
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "text-embedding-x",
		endpointType:    endpointTypeEmbeddings,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: passthroughPromptTextBytes([]byte(reqBody), endpointTypeEmbeddings),
	}
	if logData.promptTextBytes != 400 {
		t.Fatalf("sizer produced %d bytes for the probe body, want 400", logData.promptTextBytes)
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":"` + strings.Repeat("a", passthroughJSONBufferCap) + `"}`)),
	}
	rec := httptest.NewRecorder()
	h.serveBufferedJSONPassthrough(rec, st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-x"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	const wantCharge = 100 // 400 prompt bytes at the conventional 4 bytes per token
	if got := singleAddTokens(t, vkRepo); got != wantCharge {
		t.Errorf("charged %d tokens against the key, want %d", got, wantCharge)
	}
	if logData.tokensPrompt != 0 || logData.tokensCompletion != 0 {
		t.Errorf("request log usage = (%d,%d), want (0,0): an estimate is not measured usage",
			logData.tokensPrompt, logData.tokensCompletion)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
