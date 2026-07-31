package proxy

import (
	"strings"
	"testing"
)

// TestDeriveStreamError_SanitizesProviderMessage covers the streaming half of
// the upstream-error contract. The non-streaming path has always run the
// upstream body through util.SanitizeLogBody, but an in-stream SSE error was
// taken verbatim from the provider into request_logs.error_message: uncapped and
// with UUIDs intact. A provider is free to quote the request back inside an
// error, so this path could carry request content into the log and could be
// handed an unbounded string by a hostile or broken upstream.
func TestDeriveStreamError_SanitizesProviderMessage(t *testing.T) {
	t.Parallel()

	t.Run("caps an unbounded provider message", func(t *testing.T) {
		t.Parallel()

		st := &streamState{lastErrMsg: strings.Repeat("A", 50000)}
		logData := &requestLogData{statusCode: 500}

		got := deriveStreamError(st, nil, streamOptions{}, logData)

		if len([]rune(got)) > 10001 { // 10000 + the ellipsis SanitizeLogBody appends
			t.Errorf("provider message was not capped: got %d runes", len([]rune(got)))
		}
		if !strings.HasSuffix(got, "…") {
			t.Error("expected the truncation marker SanitizeLogBody appends")
		}
	})

	t.Run("redacts UUIDs the provider echoed", func(t *testing.T) {
		t.Parallel()

		st := &streamState{lastErrMsg: `request 3f2504e0-4f89-11d3-9a0c-0305e82c3301 failed`}
		logData := &requestLogData{statusCode: 500}

		got := deriveStreamError(st, nil, streamOptions{}, logData)

		if strings.Contains(got, "3f2504e0-4f89-11d3-9a0c-0305e82c3301") {
			t.Errorf("UUID survived sanitisation: %q", got)
		}
		if !strings.Contains(got, "[REDACTED]") {
			t.Errorf("expected the UUID to be redacted, got %q", got)
		}
	})

	t.Run("classifies an in-stream retired-model error", func(t *testing.T) {
		t.Parallel()

		// modelID must be set: a retirement verdict now requires the body to be
		// the provider talking about the model this request actually asked for.
		st := &streamState{lastErrMsg: "Model gemini-3-pro is not supported"}
		logData := &requestLogData{statusCode: 401, modelID: "gemini-3-pro"}

		deriveStreamError(st, nil, streamOptions{}, logData)

		if logData.errorKind != KindProviderModelGone {
			t.Errorf("errorKind = %q, want %q", logData.errorKind, KindProviderModelGone)
		}
	})

	// A client that receives an error chunk and hangs up is the ordinary case,
	// not an edge one, and the disconnect overwrites errorKind on its way out.
	// If the retirement verdict were read from errorKind, the client would be
	// suppressing the evidence by reacting to it, and a retired model would stay
	// routable for as long as clients kept disconnecting on its errors.
	t.Run("a client hangup does not erase the provider's retirement verdict", func(t *testing.T) {
		t.Parallel()

		st := &streamState{
			lastErrMsg:         "Model gemini-2.0-flash is no longer available",
			clientDisconnected: true,
		}
		logData := &requestLogData{statusCode: 404, modelID: "gemini-2.0-flash"}

		got := deriveStreamError(st, nil, streamOptions{}, logData)

		// The recorded kind still describes how the request ended.
		if logData.errorKind != KindClientDisconnect {
			t.Errorf("errorKind = %q, want %q", logData.errorKind, KindClientDisconnect)
		}
		if got != "client disconnected" {
			t.Errorf("errorMessage = %q, want the disconnect message", got)
		}
		// What the provider said about the model survives alongside it.
		if logData.upstreamKind != KindProviderModelGone {
			t.Errorf("upstreamKind = %q, want %q", logData.upstreamKind, KindProviderModelGone)
		}
		if v := verdictForStream(logData.errorKind, logData.upstreamKind, false); v != verdictGone {
			t.Errorf("verdict = %v, want verdictGone: a retirement must still be recorded", v)
		}
	})

	t.Run("an unclassifiable in-stream error stays provider_error", func(t *testing.T) {
		t.Parallel()

		st := &streamState{lastErrMsg: "Upstream request failed"}
		logData := &requestLogData{statusCode: 500}

		deriveStreamError(st, nil, streamOptions{}, logData)

		if logData.errorKind != KindProviderError {
			t.Errorf("errorKind = %q, want %q", logData.errorKind, KindProviderError)
		}
	})
}
