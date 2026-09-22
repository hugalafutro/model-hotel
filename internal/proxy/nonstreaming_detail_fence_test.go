package proxy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// The decode-error detail is stored (request_logs.error_message, the attempt
// trail), so the upstream Content-Type it quotes is bounded and fenced: a
// provider that echoes the prompt into the header does not get it into a row.
func TestNonStreamingFailureDetail_FencesTheContentTypeHeader(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain; prompt=" + canary}}}
	fence := newContentFence(chatBody(canary))
	_, detail, kind, _ := nonStreamingFailureDetail(context.Background(), resp, []byte("<html>"), nil, errors.New("invalid character '<'"), "m", fence)
	if kind != KindProviderError {
		t.Fatalf("kind = %q, want %q", kind, KindProviderError)
	}
	if strings.Contains(detail, canary) || !strings.Contains(detail, contentWithheld) {
		t.Fatalf("detail carries the prompt or lacks the marker: %s", detail)
	}
	plain := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}}
	_, detail, _, _ = nonStreamingFailureDetail(context.Background(), plain, []byte("<html>"), nil, errors.New("invalid character '<'"), "m", fence)
	if !strings.Contains(detail, `content_type="text/html"`) {
		t.Fatalf("an ordinary content type must be kept verbatim: %s", detail)
	}
}

// The read-error detail lands in the same stored places as the decode-error one
// beside it, and an upstream that dies mid-body can put its own text in that
// error, so it takes the same pass. This is the branch the Content-Type test
// above did not reach.
func TestNonStreamingFailureDetail_FencesTheReadError(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
	fence := newContentFence(chatBody(canary))

	_, detail, kind, _ := nonStreamingFailureDetail(context.Background(), resp, []byte("{"), errors.New("read tcp: "+canary), nil, "m", fence)
	if kind != KindProviderError {
		t.Fatalf("kind = %q, want %q", kind, KindProviderError)
	}
	if strings.Contains(detail, canary) || !strings.Contains(detail, contentWithheld) {
		t.Fatalf("detail carries the prompt or lacks the marker: %s", detail)
	}

	_, detail, _, _ = nonStreamingFailureDetail(context.Background(), resp, []byte("{"), errors.New("connection reset by peer"), nil, "m", fence)
	if !strings.Contains(detail, "connection reset by peer") {
		t.Fatalf("an ordinary read error must be kept verbatim: %s", detail)
	}
}

// fence() is nil-safe and masks() is its counterpart: a path that builds a
// fenced line before the log row exists must keep the regex layer rather than
// dereference nil.
func TestRequestLogData_MasksIsNilSafe(t *testing.T) {
	t.Parallel()
	var nilData *requestLogData
	if got := fencedFrameMessage(nilData.fence(), nilData.masks(), "plain upstream text"); got != "plain upstream text" {
		t.Fatalf("a nil log entry should still render the text, got %q", got)
	}
	held := &requestLogData{masker: newCredentialMasker("sk-" + strings.Repeat("a", 40))}
	if got := fencedFrameMessage(held.fence(), held.masks(), "key sk-"+strings.Repeat("a", 40)+" leaked"); strings.Contains(got, strings.Repeat("a", 40)) {
		t.Fatalf("the credential survived the masker: %q", got)
	}
}
