package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
func TestIngest_ModelTooLong_MiddlewarePreparsed(t *testing.T) {
	h := newIntegrationHandler()
	model, prefix := oversizedModelWithPrefix()
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

// TestIngest_ModelTooLong_Multipart covers the form-field path of the audio
// endpoints, where the model is parsed out of the multipart body.
func TestIngest_ModelTooLong_Multipart(t *testing.T) {
	h := newIntegrationHandler()
	model, prefix := oversizedModelWithPrefix()
	body := "--xyz\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n" + model + "\r\n--xyz--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/audio/transcriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.AudioTranscriptions(rr, req)

	assertRefusal(t, rr)
	assertBoundedRow(t, findModelRow(t, h, prefix), model)
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

// TestFailRequest_BoundsErrorMessage pins the clamp at the sink: a caller
// handing failRequest an oversized message gets a bounded row, whatever the
// caller was.
func TestFailRequest_BoundsErrorMessage(t *testing.T) {
	h := newIntegrationHandler()
	prefix := "clamp-" + uuid.NewString()[:8]
	req := withAuthContext(httptest.NewRequest(http.MethodPost, "/chat/completions", http.NoBody))
	logData, _ := h.newPendingRequestLog(req, endpointTypeChat, prefix, false)
	h.failRequest(logData, http.StatusBadGateway, KindProviderError, strings.Repeat("x", maxLogMessageRunes*2), 0, time.Now(), 0, resolveTimings{}, resolveCacheHits{}, 0)

	row := findModelRow(t, h, prefix)
	if n := utf8.RuneCountInString(row.errorMessage); n != maxLogMessageRunes+1 {
		t.Errorf("stored error_message is %d runes, want %d plus the ellipsis", n, maxLogMessageRunes)
	}
	if row.errorKind != string(KindProviderError) {
		t.Errorf("error_kind = %q, want %q", row.errorKind, KindProviderError)
	}
}
