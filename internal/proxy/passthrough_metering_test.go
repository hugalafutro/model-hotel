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
	h.serveBufferedJSONPassthrough(rec, httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
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

// TestPassthrough_NoUsageBlockStillMeters is the sibling of the oversized case,
// and the half the first fix left behind. A normal-sized pass-through response
// that carries no "usage" block extracts (0,0), and the guard below it only
// charged when one of them was non-zero, so nothing was metered at all.
//
// Image generation returns JSON and routinely reports no usage, so it is the
// family this branch actually rescues. Text-to-speech does NOT reach here: it
// answers audio/mpeg and routes to serveStreamedPassthrough, which is covered
// by TestStreamedPassthrough_BinaryResponseIsMetered. Labelling this case as
// TTS, as an earlier version did, described a JSON response TTS never returns.
func TestPassthrough_NoUsageBlockStillMeters(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	reqBody := `{"model":"dall-e-3","prompt":"` + strings.Repeat("s", 400) + `"}`
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "dall-e-3",
		endpointType:    endpointTypeImage,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: passthroughPromptTextBytes([]byte(reqBody), endpointTypeImage),
	}
	if logData.promptTextBytes != 400 {
		t.Fatalf("probe body sized %d, want 400", logData.promptTextBytes)
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	// A perfectly ordinary provider response with no usage block.
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"created":1,"data":[{"b64":"AAAA"}]}`)),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "dall-e-3"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	const wantCharge = 100 // 400 prompt bytes at 4 bytes per token
	if got := singleAddTokens(t, vkRepo); got != wantCharge {
		t.Errorf("charged %d tokens, want %d: a response without usage must still meter", got, wantCharge)
	}
	if logData.tokensPrompt != 0 {
		t.Errorf("request log prompt = %d, want 0: an estimate is not measured usage", logData.tokensPrompt)
	}
}

// TestPassthrough_ReportedUsageWinsOverEstimate keeps the estimate from
// displacing a real figure: when the provider does report usage, that is what
// gets charged and what the request log records.
func TestPassthrough_ReportedUsageWinsOverEstimate(t *testing.T) {
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
		promptTextBytes: 4000, // an estimate here would be 1000 tokens
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":7,"total_tokens":7}}`)),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-x"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	if got := singleAddTokens(t, vkRepo); got != 7 {
		t.Errorf("charged %d, want the provider's reported 7", got)
	}
	if logData.tokensPrompt != 7 {
		t.Errorf("request log prompt = %d, want the measured 7", logData.tokensPrompt)
	}
}

// TestStreamedPassthrough_BinaryResponseIsMetered covers where text-to-speech
// actually lands. /v1/audio/speech answers audio/mpeg, which is neither JSON nor
// SSE, so it routes to serveStreamedPassthrough — and there the SSE tail that
// carries usage is only allocated for SSE, so the usage report is structurally
// always absent for audio. Guarding the debit on a report that can never arrive
// left TTS unmetered on every single request.
//
// A previous version of this fix claimed to have rescued TTS while only touching
// the JSON branch, and its test constructed a JSON response TTS never returns.
func TestStreamedPassthrough_BinaryResponseIsMetered(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	reqBody := `{"model":"tts-1","input":"` + strings.Repeat("s", 400) + `","voice":"alloy"}`
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "tts-1",
		endpointType:    endpointTypeTTS,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: passthroughPromptTextBytes([]byte(reqBody), endpointTypeTTS),
	}
	if logData.promptTextBytes != 400 {
		t.Fatalf("probe body sized %d, want 400", logData.promptTextBytes)
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	// Real shape: binary audio, no usage anywhere.
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(strings.NewReader("ID3\x04\x00\x00\x00fake mp3 payload")),
	}
	req := httptest.NewRequest("POST", "/v1/audio/speech", http.NoBody)
	h.serveStreamedPassthrough(httptest.NewRecorder(), req, st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "tts-1"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "audio/mpeg", false, 1, 10.0)

	const wantCharge = 100 // 400 prompt bytes at 4 bytes per token
	if got := singleAddTokens(t, vkRepo); got != wantCharge {
		t.Errorf("charged %d tokens for a TTS request, want %d: binary passthrough must meter too", got, wantCharge)
	}
}

// TestPassthrough_EmptyAnswerIsNotCharged is the gate on the estimate. An
// aggregator in front of a retired model answers 200 with `{"data":[]}` — the
// case passthroughAnswered exists to detect — and that costs the operator
// nothing, so charging a full prompt estimate for it would bill the caller for
// every empty reply. estimateMissingUsage states the same rule for streams:
// nothing is estimated when nothing was delivered.
func TestPassthrough_EmptyAnswerIsNotCharged(t *testing.T) {
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
		promptTextBytes: 4000,
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-x"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	if len(vkRepo.addTokensCalls) != 0 {
		t.Errorf("charged %d times for an empty answer, want 0", len(vkRepo.addTokensCalls))
	}
}

// TestMultipartPromptTextBytes_SizesFormFieldsNotTheUpload keeps the multipart
// families from being the next silent no-op: their prompt is a form field, and
// leaving promptTextBytes at zero would make every estimate downstream charge
// nothing, exactly as the chat-only sizer did for the JSON families.
func TestMultipartPromptTextBytes_SizesFormFieldsNotTheUpload(t *testing.T) {
	parts := []multipartPart{
		{fieldName: "model", data: []byte("whisper-1")},                       // routing
		{fieldName: "language", data: []byte("en")},                           // config
		{fieldName: "response_format", data: []byte("verbose_json")},          // config
		{fieldName: "temperature", data: []byte("0")},                         // config
		{fieldName: "prompt", data: []byte("transcribe carefully")},           // 20 bytes, the only prompt
		{fieldName: "file", fileName: "audio.mp3", data: make([]byte, 5<<20)}, // the upload
	}
	if got, want := multipartPromptTextBytes(parts), 20; got != want {
		t.Errorf("sized %d, want %d: only the prompt field counts", got, want)
	}
}

// TestMultipartPromptTextBytes_MetadataOnlyFormSizesNothing is the overcharge
// guard. Every field on these forms except "prompt" is configuration, so a
// request that sends only options and a file must size to zero PROMPT BYTES: an
// allowlist keeps a newly added provider parameter from silently becoming
// billable text.
//
// Zero bytes is not zero charge. This is the exact shape that made a served
// transcription free, so the charge path applies minPassthroughTokens on top —
// see TestMultipartPassthrough_NoPromptFieldStillMeters. The two are separate
// on purpose: the sizer says what text the caller sent, the charge decides what
// a delivered request costs.
func TestMultipartPromptTextBytes_MetadataOnlyFormSizesNothing(t *testing.T) {
	parts := []multipartPart{
		{fieldName: "model", data: []byte("whisper-1")},
		{fieldName: "language", data: []byte("en")},
		{fieldName: "response_format", data: []byte("verbose_json")},
		{fieldName: "temperature", data: []byte("0")},
		{fieldName: "timestamp_granularities[]", data: []byte("word")},
		{fieldName: "size", data: []byte("1024x1024")},
		{fieldName: "n", data: []byte("1")},
		{fieldName: "file", fileName: "audio.mp3", data: make([]byte, 1<<20)},
	}
	if got := multipartPromptTextBytes(parts); got != 0 {
		t.Errorf("sized %d, want 0: configuration fields are not prompt text and must not be charged", got)
	}
}

// TestMultipartPassthrough_NoPromptFieldStillMeters closes the free-request
// hole the allowlist above left behind.
//
// multipartPromptTextBytes counts only the "prompt" form field, correctly: every
// other field is configuration and charging for it bills the caller for their
// own options. But "prompt" is OPTIONAL on transcriptions and translations, and
// /images/variations has no such field at all, so the ordinary shape of these
// requests sizes as zero. The estimate then came out zero, nothing was debited,
// and a served transcription cost the key nothing at all — no tokens_used, no
// TPM draw — on every request.
//
// A served pass-through request is never free. The upload still is not measured
// (see multipartPromptTextBytes: it is the payload, not the prompt, and sizing
// it would invent an enormous charge), so what is charged is a floor, not a
// proportional estimate.
func TestMultipartPassthrough_NoPromptFieldStillMeters(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	// Exactly what ingestMultipartRequest produces for a transcription that
	// sends only a file and its options: no prompt field, so zero prompt bytes.
	parts := []multipartPart{
		{fieldName: "model", data: []byte("whisper-1")},
		{fieldName: "response_format", data: []byte("json")},
		{fieldName: "file", fileName: "audio.mp3", data: make([]byte, 3<<20)},
	}
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "whisper-1",
		endpointType:    endpointTypeSTT,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: multipartPromptTextBytes(parts),
	}
	if logData.promptTextBytes != 0 {
		t.Fatalf("probe sized %d prompt bytes, want 0 (the case under test)", logData.promptTextBytes)
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	// A real transcription answer, with no usage block (the common case).
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"text":"hello there"}`)),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "whisper-1"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	got := singleAddTokens(t, vkRepo)
	if got == 0 {
		t.Fatal("charged 0 tokens: a served transcription with no prompt field is free, evading tokens_used and the TPM budget")
	}
	// Literal, not minPassthroughTokens: comparing the charge against the same
	// constant the code uses would pass for any value of it, including one large
	// enough to overcharge every zero-prompt request. The floor is a
	// billing-visible number, so changing it should break a test.
	if got != 1 {
		t.Errorf("charged %d tokens, want 1 (minPassthroughTokens)", got)
	}
	if logData.tokensPrompt != 0 {
		t.Errorf("request log prompt = %d, want 0: a floor is not measured usage", logData.tokensPrompt)
	}
}

// TestMultipartPassthrough_FloorDoesNotDisplaceRealFigures keeps the floor from
// becoming a ceiling or a substitute: a request that does carry prompt text is
// charged its estimate, and a provider that reports usage is charged that.
func TestMultipartPassthrough_FloorDoesNotDisplaceRealFigures(t *testing.T) {
	newLog := func(promptBytes int) *requestLogData {
		return &requestLogData{
			id:              uuid.New().String(),
			modelID:         "whisper-1",
			endpointType:    endpointTypeSTT,
			virtualKeyName:  "test-key",
			virtualKeyID:    "00000000-0000-0000-0000-000000000001",
			state:           "streaming",
			promptTextBytes: promptBytes,
		}
	}

	t.Run("a sized prompt is charged its estimate, not the floor", func(t *testing.T) {
		h := newIntegrationHandler()
		t.Cleanup(func() { stopUnitHandler(h) })
		vkRepo := &mockVirtualKeyRepo{}
		h.virtualKeyRepo = vkRepo

		logData := newLog(400) // 100 tokens, far above the floor
		st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
		h.insertRequestLogAsync(logData)
		time.Sleep(20 * time.Millisecond)

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"text":"ok"}`)),
		}
		h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
			model:    &model.Model{ID: uuid.New(), ModelID: "whisper-1"},
			provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
		}, resp, "application/json", 1, 10.0)

		if got := singleAddTokens(t, vkRepo); got != 100 {
			t.Errorf("charged %d, want 100: the floor must not replace a real estimate", got)
		}
	})

	t.Run("reported usage wins over the floor", func(t *testing.T) {
		h := newIntegrationHandler()
		t.Cleanup(func() { stopUnitHandler(h) })
		vkRepo := &mockVirtualKeyRepo{}
		h.virtualKeyRepo = vkRepo

		logData := newLog(0)
		st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
		h.insertRequestLogAsync(logData)
		time.Sleep(20 * time.Millisecond)

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"text":"ok","usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)),
		}
		h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
			model:    &model.Model{ID: uuid.New(), ModelID: "whisper-1"},
			provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
		}, resp, "application/json", 1, 10.0)

		if got := singleAddTokens(t, vkRepo); got != 10 {
			t.Errorf("charged %d, want 10: a reported figure always wins", got)
		}
	})
}

// TestPassthroughFloor_StaysBehindTheDeliveryGate keeps the floor from undoing
// the gate it sits behind. An aggregator in front of a retired model answering
// 200 with `{"data":[]}` served nothing, and charging even one token there would
// bill the caller on every empty answer — the regression the gate was added to
// prevent.
//
// Embeddings is the endpoint this can be shown on: passthroughAnswered only
// inspects the body for that type. For the multipart and binary families any
// non-empty 200 body counts as delivered, so a junk answer there does draw the
// floor. That is the intended trade — one token is the price of not being able
// to tell a bad transcription from a good one, and per-key RPS limiting bounds
// how often it can be paid.
func TestPassthroughFloor_StaysBehindTheDeliveryGate(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "text-embedding-3-small",
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
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	if n := len(vkRepo.addTokensCalls); n != 0 {
		t.Errorf("charged %d times for an empty answer, want 0: the floor must not defeat the delivery gate", n)
	}
}

// TestOversizedPassthrough_ZeroPromptStillMeters covers the sibling branch the
// first attempt at the floor missed.
//
// serveBufferedJSONPassthrough charges in two places: the ordinary path, and an
// oversized path that streams past the 8 MiB cap with usage extraction skipped.
// The oversized one hand-rolled its own estimate behind the identical
// `if estimated > 0` guard, so the zero-prompt families were still free there
// after the floor was added below it. /images/variations has no prompt field at
// all and four b64_json images clear the cap routinely, so the endpoint that
// motivated the fix stayed free on exactly the requests that cost the most.
func TestOversizedPassthrough_ZeroPromptStillMeters(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "dall-e-2",
		endpointType:    endpointTypeImage,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 0, // /images/variations carries no prompt field
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	// A body past passthroughJSONBufferCap, as four b64_json images produce.
	huge := `{"created":1,"data":[{"b64_json":"` + strings.Repeat("A", passthroughJSONBufferCap+16) + `"}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(huge)),
	}
	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "dall-e-2"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "application/json", 1, 10.0)

	got := singleAddTokens(t, vkRepo)
	if got == 0 {
		t.Fatal("charged 0 tokens: an oversized answer with no prompt text is free, on the costliest requests")
	}
	if got != 1 {
		t.Errorf("charged %d tokens, want 1 (minPassthroughTokens)", got)
	}
}

// TestStreamedPassthrough_ZeroPromptStillMeters pins the floor on the OTHER
// serve path. A transcription with response_format=text|srt|vtt answers
// text/plain, which is neither JSON nor SSE, so it routes to
// serveStreamedPassthrough — a branch none of the other floor tests touch.
// Without this, a refactor that inlined the floor into the buffered branch
// would keep the suite green while re-freeing every non-JSON transcription.
func TestStreamedPassthrough_ZeroPromptStillMeters(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "whisper-1",
		endpointType:    endpointTypeSTT,
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
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("hello there, this is the transcript")),
	}
	h.serveStreamedPassthrough(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", http.NoBody),
		st, modelCandidate{
			model:    &model.Model{ID: uuid.New(), ModelID: "whisper-1"},
			provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
		}, resp, "text/plain", false, 1, 10.0)

	got := singleAddTokens(t, vkRepo)
	if got == 0 {
		t.Fatal("charged 0 tokens: a response_format=text transcription answers text/plain and is free on the streamed path")
	}
	if got != 1 {
		t.Errorf("charged %d tokens, want 1 (minPassthroughTokens)", got)
	}
}
