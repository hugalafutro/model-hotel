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
