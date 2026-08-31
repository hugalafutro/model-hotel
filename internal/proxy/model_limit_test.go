package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// oversizedModelWithPrefix builds a model one rune past the bound. The random
// prefix is how the test finds its own request_logs row afterwards; it must
// fit inside the excerpt the row keeps.
func oversizedModelWithPrefix() (model, prefix string) {
	prefix = "toolong-" + uuid.NewString()[:8] + "-"
	return prefix + strings.Repeat("a", maxModelNameRunes+1-utf8.RuneCountInString(prefix)), prefix
}

// incompressibleModelWithPrefix builds a model of the given length out of
// random hex, so it stays large after TOAST compression. request_logs.model_id
// is btree-indexed and Postgres refuses an index entry over roughly 2.7 KB of
// compressed bytes; a run of one repeated byte compresses under that and is
// accepted, random hex is not. That is what makes the fixture discriminate:
// a guard that lets this model reach the pending INSERT loses the row.
func incompressibleModelWithPrefix(runes int) (model, prefix string) {
	prefix = "toolong-" + uuid.NewString()[:8] + "-"
	var b strings.Builder
	b.WriteString(prefix)
	for b.Len() < runes {
		b.WriteString(strings.ReplaceAll(uuid.NewString(), "-", ""))
	}
	return b.String()[:runes], prefix
}

// modelRow is what the request-log row looks like after ingest refused it.
type modelRow struct {
	modelID, state, errorKind, errorMessage string
}

// findModelRow polls for the row whose model_id starts with prefix. The
// pending INSERT and the terminal UPDATE are both asynchronous, so the row can
// exist before it is closed; the poll waits for the closed state.
func findModelRow(t *testing.T, h *Handler, prefix string) modelRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var row modelRow
		err := h.dbPool.QueryRow(context.Background(),
			`SELECT model_id, state, COALESCE(error_kind, ''), COALESCE(error_message, '')
			 FROM request_logs WHERE model_id LIKE $1 || '%' ORDER BY created_at DESC LIMIT 1`, prefix,
		).Scan(&row.modelID, &row.state, &row.errorKind, &row.errorMessage)
		if err == nil && row.state == "failed" {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("no closed request_logs row with model_id prefix %q (last err: %v, last state: %q)", prefix, err, row.state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertRefusal checks the response is the constant refusal and nothing more:
// a 400, the bounded message, and a body far smaller than the model it refused.
func assertRefusal(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %.200s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not the OpenAI error shape: %v; body: %.200s", err, rr.Body.String())
	}
	if resp.Error.Message != modelTooLongMessage {
		t.Errorf("message = %q, want %q", resp.Error.Message, modelTooLongMessage)
	}
	if n := rr.Body.Len(); n > 256 {
		t.Errorf("response body is %d bytes: the refusal must not echo the model", n)
	}
}

// assertBoundedRow checks the refusal left an attributed row carrying only the
// excerpt, the validation kind, and the constant message.
func assertBoundedRow(t *testing.T, row modelRow, model string) {
	t.Helper()
	if want := modelExcerpt(model); row.modelID != want {
		t.Errorf("model_id = %q (%d runes), want the excerpt %q", row.modelID, utf8.RuneCountInString(row.modelID), want)
	}
	if row.errorKind != string(KindValidation) {
		t.Errorf("error_kind = %q, want %q", row.errorKind, KindValidation)
	}
	if row.errorMessage != modelTooLongMessage {
		t.Errorf("error_message = %q, want %q", row.errorMessage, modelTooLongMessage)
	}
}

// TestIngest_ModelTooLong_BodyParsed covers the fallback path: no middleware
// pre-parse, the model comes out of the body after the pending row was
// inserted with an empty model. The refusal must close that row with the
// excerpt, never the field.
func TestIngest_ModelTooLong_BodyParsed(t *testing.T) {
	h := newIntegrationHandler()
	model, prefix := oversizedModelWithPrefix()
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[]}`))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	assertRefusal(t, rr)
	assertBoundedRow(t, findModelRow(t, h, prefix), model)
}

// TestIngest_ModelTooLong_MiddlewarePreparsed covers the path every /v1 JSON
// route and /api/chat take in production: streamingAwareTimeout has already
// put the model in the context, so the pending INSERT would carry it. The row
// must still exist (the refusal stays attributed to the key) and carry the
// excerpt.
//
// The fixture is 8 KB of random hex on purpose. If the guard before the INSERT
// regresses, the INSERT carries the field, the btree index refuses it, the
// terminal UPDATE finds no row, and this test times out waiting for one; a
// compressible fixture would be accepted by the index and let the terminal
// UPDATE rewrite model_id to the excerpt, hiding the regression.
func TestIngest_ModelTooLong_MiddlewarePreparsed(t *testing.T) {
	h := newIntegrationHandler()
	model, prefix := incompressibleModelWithPrefix(8192)
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[]}`))
	req = withAuthContext(req)
	ctx := context.WithValue(req.Context(), ctxkeys.RequestModelKey, model)
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, true)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	assertRefusal(t, rr)
	assertBoundedRow(t, findModelRow(t, h, prefix), model)
}

// TestIngest_ModelTooLong_Messages covers the native Anthropic ingress, which
// sets the context model itself and answers in the Anthropic error envelope.
func TestIngest_ModelTooLong_Messages(t *testing.T) {
	h := newIntegrationHandler()
	model, prefix := oversizedModelWithPrefix()
	body := `{"model":"` + model + `","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %.200s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Type  string `json:"type"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.Type != "error" {
		t.Fatalf("response is not the Anthropic error envelope (err %v); body: %.200s", err, rr.Body.String())
	}
	if resp.Error.Message != modelTooLongMessage {
		t.Errorf("message = %q, want %q", resp.Error.Message, modelTooLongMessage)
	}
	if n := rr.Body.Len(); n > 256 {
		t.Errorf("response body is %d bytes: the refusal must not echo the model", n)
	}
	assertBoundedRow(t, findModelRow(t, h, prefix), model)
}

// TestIngest_ModelTooLong_Multipart covers the form-field path of the audio
// endpoints, where the model is parsed out of the multipart body.
func TestIngest_ModelTooLong_Multipart(t *testing.T) {
	h := newIntegrationHandler()
	model, prefix := oversizedModelWithPrefix()
	req := multipartModelRequest(model)

	rr := httptest.NewRecorder()
	h.AudioTranscriptions(rr, req)

	assertRefusal(t, rr)
	assertBoundedRow(t, findModelRow(t, h, prefix), model)
}

// TestIngest_MultipartModel_InvalidUTF8Normalized pins the form field's UTF-8
// normalisation: the JSON paths get valid UTF-8 from encoding/json, the form
// field is raw bytes, and Postgres refuses an invalid byte in model_id, which
// would lose the row. The byte must be replaced, and the row must land.
func TestIngest_MultipartModel_InvalidUTF8Normalized(t *testing.T) {
	h := newIntegrationHandler()
	prefix := "badutf8-" + uuid.NewString()[:8] + "-"
	req := multipartModelRequest(prefix + "\x80\x80")

	rr := httptest.NewRecorder()
	h.AudioTranscriptions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 from format validation; body: %.200s", rr.Code, rr.Body.String())
	}
	row := findModelRow(t, h, prefix)
	// One replacement for the run of two invalid bytes: that is
	// strings.ToValidUTF8's contract, a run, not a byte.
	if want := prefix + "�"; row.modelID != want {
		t.Errorf("model_id = %q, want the replacement character %q", row.modelID, want)
	}
}

// multipartModelRequest builds an audio transcription request whose only form
// field is the model.
func multipartModelRequest(model string) *http.Request {
	body := "--xyz\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n" + model + "\r\n--xyz--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/audio/transcriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	return withAuthContext(req)
}

// TestIngest_ModelAtLimit_ReachesFormatValidation pins that the bound does not
// over-reject: a model of exactly maxModelNameRunes runes, in multi-byte
// characters so a byte count would refuse it, passes the length guard and is
// refused by the ordinary format validation instead.
func TestIngest_ModelAtLimit_ReachesFormatValidation(t *testing.T) {
	h := newIntegrationHandler()
	model := strings.Repeat("é", maxModelNameRunes)
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[]}`))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 from format validation", rr.Code)
	}
	if strings.Contains(rr.Body.String(), modelTooLongMessage) {
		t.Errorf("an at-limit model must not hit the length refusal; body: %.200s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid model format") {
		t.Errorf("want the format refusal, got: %.200s", rr.Body.String())
	}
}

func TestModelTooLong_CountsRunesNotBytes(t *testing.T) {
	if modelTooLong("") {
		t.Error("empty model is not too long")
	}
	if modelTooLong(strings.Repeat("é", maxModelNameRunes)) {
		t.Errorf("%d multi-byte runes are at the bound, not over it", maxModelNameRunes)
	}
	if !modelTooLong(strings.Repeat("é", maxModelNameRunes+1)) {
		t.Errorf("%d runes are over the bound", maxModelNameRunes+1)
	}
}

// TestModelTooLongMessage_SpellsTheBound keeps the constant message honest
// about the number it quotes.
func TestModelTooLongMessage_SpellsTheBound(t *testing.T) {
	if !strings.Contains(modelTooLongMessage, strconv.Itoa(maxModelNameRunes)) {
		t.Errorf("message %q does not spell the bound %d", modelTooLongMessage, maxModelNameRunes)
	}
}

func TestModelExcerpt(t *testing.T) {
	short := "openai/gpt-4o"
	if got := modelExcerpt(short); got != short {
		t.Errorf("short model changed: %q", got)
	}
	exact := strings.Repeat("é", modelExcerptRunes)
	if got := modelExcerpt(exact); got != exact {
		t.Errorf("model at the excerpt length changed: %q", got)
	}
	long := strings.Repeat("é", modelExcerptRunes+1)
	got := modelExcerpt(long)
	if !utf8.ValidString(got) {
		t.Fatalf("excerpt is not valid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") || utf8.RuneCountInString(got) != modelExcerptRunes+1 {
		t.Errorf("excerpt = %d runes %q, want %d runes plus an ellipsis", utf8.RuneCountInString(got), got, modelExcerptRunes)
	}
	if !strings.HasPrefix(got, strings.Repeat("é", modelExcerptRunes)) {
		t.Errorf("excerpt must be the model's own prefix, got %q", got)
	}
}

func TestTruncateLogMessage(t *testing.T) {
	short := "provider request failed"
	if got := truncateLogMessage(short); got != short {
		t.Errorf("short message changed: %q", got)
	}
	exact := strings.Repeat("é", maxLogMessageRunes)
	if got := truncateLogMessage(exact); got != exact {
		t.Errorf("message at the bound changed")
	}
	long := strings.Repeat("é", maxLogMessageRunes+50)
	got := truncateLogMessage(long)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated message is not valid UTF-8")
	}
	if n := utf8.RuneCountInString(got); n != maxLogMessageRunes+1 {
		t.Errorf("truncated length = %d runes, want %d plus the ellipsis", n, maxLogMessageRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated message must end with an ellipsis")
	}
}

// TestUpdateRequestLog_BoundsErrorMessage pins the clamp at the sink. The
// message is assigned directly, the way the stream finaliser and the native
// readers do it, bypassing failRequest: the bound must still hold.
func TestUpdateRequestLog_BoundsErrorMessage(t *testing.T) {
	h := newIntegrationHandler()
	prefix := "clamp-" + uuid.NewString()[:8]
	req := withAuthContext(httptest.NewRequest(http.MethodPost, "/chat/completions", http.NoBody))
	logData, _ := h.newPendingRequestLog(req, endpointTypeChat, prefix, false)
	logData.statusCode = http.StatusBadGateway
	logData.errorKind = KindProviderError
	logData.errorMessage = strings.Repeat("x", maxLogMessageRunes*2)
	logData.state = "failed"
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

	row := findModelRow(t, h, prefix)
	if n := utf8.RuneCountInString(row.errorMessage); n != maxLogMessageRunes+1 {
		t.Errorf("stored error_message is %d runes, want %d plus the ellipsis", n, maxLogMessageRunes)
	}
	if row.errorKind != string(KindProviderError) {
		t.Errorf("error_kind = %q, want %q", row.errorKind, KindProviderError)
	}
}
