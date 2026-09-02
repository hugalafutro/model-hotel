package proxy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// The native Anthropic emit recognises error text by the wrapper's type OR by
// a populated error member on any event, and masks every held provider key
// out of it; a content event keeps to the candidate's own key.
func TestEmitRawData_HeldKeyInEveryErrorShape(t *testing.T) {
	const foreign = "custom-key-A-native-11112222-33334444"
	const own = "custom-key-B-native-77776666-88889999"
	util.HoldSecret(foreign)
	h := &Handler{}
	for _, tc := range []struct {
		name, payload string
		wantMasked    bool
	}{
		{"typed error event", `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key ` + foreign + `"}}`, true},
		{"bare error member", `{"error":{"type":"authentication_error","message":"invalid x-api-key ` + foreign + `"}}`, true},
		{"error member on an ordinary event", `{"type":"message_delta","error":{"message":"relay: bad api key ` + foreign + `"}}`, true},
		{"content keeps to the candidate", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"` + foreign + ` and ` + own + `"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			sink := newStreamSink(rec)
			st := &streamState{masker: newCredentialMasker(own)}
			if stop := h.emitRawData(sink, st, sseEvent{raw: []byte("data: " + tc.payload + "\n\n"), payload: tc.payload}, 1, &requestLogData{}); stop {
				t.Fatalf("emitRawData stopped")
			}
			out := rec.Body.String()
			if strings.Contains(out, own) {
				t.Fatalf("the candidate's own key reached the client: %q", out)
			}
			if tc.wantMasked && strings.Contains(out, foreign) {
				t.Fatalf("a held foreign key reached the client in error text: %q", out)
			}
			if !tc.wantMasked && !strings.Contains(out, foreign) {
				t.Fatalf("content was rewritten for a held key: %q", out)
			}
		})
	}
}
