package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// bodyReadRequest is a request whose body fails mid-read, on ctx, carrying a
// virtual-key name unique to the test so its row can be found.
func bodyReadRequest(ctx context.Context, keyName, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", errReader{io.ErrUnexpectedEOF})
	r.Header.Set("Content-Type", contentType)
	ctx = context.WithValue(ctx, virtualKeyNameKey, keyName)
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.NewString())
	return r.WithContext(ctx)
}

// A request body that stops arriving because the caller left is their
// disconnect: 499 on the wire and a client_disconnect row, not the 400
// validation refusal a malformed upload earns. One case per ingest path.
func TestIngest_DisconnectedBodyReadIsAClientDisconnect(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	cases := []struct {
		name        string
		contentType string
		ingest      func(w http.ResponseWriter, r *http.Request)
	}{
		{"json", "application/json", func(w http.ResponseWriter, r *http.Request) { h.ingestRequest(w, r, endpointTypeChat) }},
		{"multipart", "multipart/form-data; boundary=x", func(w http.ResponseWriter, r *http.Request) { h.ingestMultipartRequest(w, r, endpointTypeSTT) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyName := "disconnect-" + uuid.NewString()[:8]
			w := httptest.NewRecorder()
			tc.ingest(w, bodyReadRequest(cancelledContext(), keyName, tc.contentType))

			if w.Code != statusClientClosedRequest {
				t.Fatalf("status = %d, want 499; body: %s", w.Code, w.Body.String())
			}
			row := closedRowWhere(t, h, "virtual_key_name", keyName)
			if row.status != statusClientClosedRequest || row.errorKind != string(KindClientDisconnect) {
				t.Errorf("row = %+v, want 499 client_disconnect", row)
			}
		})
	}
}

// The same read failing on a live request is still the caller's broken upload.
func TestIngest_BrokenBodyReadOnALiveRequestIsStillA400(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	keyName := "broken-" + uuid.NewString()[:8]
	w := httptest.NewRecorder()
	h.ingestRequest(w, bodyReadRequest(context.Background(), keyName, "application/json"), endpointTypeChat)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if row := closedRowWhere(t, h, "virtual_key_name", keyName); row.errorKind != string(KindValidation) {
		t.Errorf("row = %+v, want the validation kind", row)
	}
}

// The two handler reads with no row to close answer by the same rule.
func TestHandlerBodyReads_DisconnectedReadAnswers499(t *testing.T) {
	h := &Handler{}
	for name, read := range map[string]func(http.ResponseWriter, *http.Request) ([]byte, bool){
		"responses": h.readRawBody,
		"anthropic": h.readAnthropicBody,
	} {
		for _, live := range []bool{true, false} {
			ctx := cancelledContext()
			want := statusClientClosedRequest
			if live {
				ctx, want = context.Background(), http.StatusBadRequest
			}
			w := httptest.NewRecorder()
			read(w, bodyReadRequest(ctx, "k", "application/json"))
			if w.Code != want {
				t.Errorf("%s (live=%v): status = %d, want %d", name, live, w.Code, want)
			}
		}
	}
}

// An upload over MAX_REQUEST_SIZE is named as one, not as a malformed form:
// the multipart read is described by describeBodyReadFault.
func TestIngestMultipart_OverCapReadIsNamedAsSuch(t *testing.T) {
	logs := captureLogsAt(t, slog.LevelWarn)
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", strings.NewReader(strings.Repeat("x", 64)))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	r.Body = http.MaxBytesReader(w, r.Body, 8)
	h.ingestMultipartRequest(w, r, endpointTypeSTT)

	lines := logs("proxy: failed to read multipart request body")
	if len(lines) != 1 || !strings.Contains(lines[0], "the body exceeded the size limit") {
		t.Fatalf("read fault lines = %q, want the size-limit class", lines)
	}
}
