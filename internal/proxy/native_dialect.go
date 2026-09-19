package proxy

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
)

// nativeDialect is a wire format the proxy forwards VERBATIM: a request that
// arrived in a vendor's own dialect and resolved to a provider speaking that
// dialect goes upstream untouched, and the answer comes back untouched. What
// the pipeline still needs from the bytes it does not translate (usage for
// metering, whether the body answered, where a stream ends, how to report a
// failure in that dialect) is read through this interface. Two dialects speak
// it: Anthropic Messages on /v1/messages and OpenAI Responses on /v1/responses.
type nativeDialect interface {
	// label names the path in log lines and reject reasons.
	label() string
	// terminalEvent is the stream event that ends a completed stream, named
	// in the truncation log line.
	terminalEvent() string
	parseUsage(body []byte) nativeUsage
	// carriesContent reports a non-streaming body holding any content item.
	carriesContent(body []byte) bool
	// stopStated reports a body that says how its generation ended (a
	// stop_reason, a status): a provider that answered, whatever it carried.
	stopStated(body []byte) bool
	textBytes(body []byte) int
	inspectStreamEvent(payload []byte) nativeStreamEvent
	// streamFailure is the terminal error frame in this dialect. responseID
	// and sequence continue the stream the client was reading, for a dialect
	// that numbers its events; the other ignores them.
	streamFailure(message, kind, responseID string, sequence int) []byte
	// writeError writes a non-streaming gateway error in this dialect.
	writeError(w http.ResponseWriter, message string, status int)
}

// nativeUsage is the metering summary of one native body or stream. The cache
// split is zero when the upstream reported no cache read.
type nativeUsage struct {
	promptTokens     int
	completionTokens int
	cacheHitTokens   int
	cacheMissTokens  int
}

// nativeStreamEvent is the decoded summary of one native stream event.
type nativeStreamEvent struct {
	eventType       string
	terminal        bool
	inputTokens     int
	hasInput        bool
	outputTokens    int
	hasOutput       bool
	cacheHitTokens  int
	cacheMissTokens int
	errorMessage    string
	carriesError    bool
	textBytes       int
	sequenceNumber  int
	hasSequence     bool
	responseID      string
}

// nativeAttempt is the dialect the current failover attempt forwards verbatim,
// nil when the attempt is translated. Set per attempt by
// buildCandidateRequest; read by the dispatch, the stream and the writer.
func (st *requestState) nativeAttempt() nativeDialect {
	switch {
	case st.anthropicNativeAttempt:
		return anthropicNative
	case st.responsesNativeAttempt:
		return responsesNative
	}
	return nil
}

// --- Anthropic Messages ---

type anthropicDialect struct{}

var anthropicNative nativeDialect = anthropicDialect{}

func (anthropicDialect) label() string         { return "native anthropic" }
func (anthropicDialect) terminalEvent() string { return "message_stop" }

func (anthropicDialect) parseUsage(body []byte) nativeUsage {
	u := anthropic.ParseResponseUsage(body)
	return nativeUsage{promptTokens: u.PromptTokens, completionTokens: u.CompletionTokens, cacheHitTokens: u.CacheHitTokens, cacheMissTokens: u.CacheMissTokens}
}

func (anthropicDialect) carriesContent(body []byte) bool {
	return anthropic.ResponseCarriesContent(body)
}
func (anthropicDialect) stopStated(body []byte) bool { return anthropic.ResponseStopReason(body) != "" }
func (anthropicDialect) textBytes(body []byte) int   { return anthropic.ResponseTextBytes(body) }

func (anthropicDialect) inspectStreamEvent(payload []byte) nativeStreamEvent {
	info := anthropic.InspectStreamEvent(payload)
	return nativeStreamEvent{
		eventType:       info.Type,
		terminal:        info.Type == "message_stop",
		inputTokens:     info.InputTokens,
		hasInput:        info.HasInput,
		outputTokens:    info.OutputTokens,
		hasOutput:       info.HasOutput,
		cacheHitTokens:  info.CacheHitTokens,
		cacheMissTokens: info.CacheMissTokens,
		errorMessage:    info.ErrorMessage,
		carriesError:    info.CarriesError,
		textBytes:       info.TextBytes,
	}
}

func (anthropicDialect) streamFailure(message, _, _ string, _ int) []byte {
	frame := append([]byte("event: error\ndata: "), anthropic.BuildErrorResponseFromMessage(message, http.StatusBadGateway)...)
	return append(frame, "\n\n"...)
}

func (anthropicDialect) writeError(w http.ResponseWriter, message string, status int) {
	writeAnthropicError(w, message, status)
}

// --- OpenAI Responses ---

type responsesDialect struct{}

var responsesNative nativeDialect = responsesDialect{}

func (responsesDialect) label() string         { return "native responses" }
func (responsesDialect) terminalEvent() string { return "response.completed" }

func (responsesDialect) parseUsage(body []byte) nativeUsage {
	u := openairesponses.ParseResponseUsage(body)
	return nativeUsage{promptTokens: u.PromptTokens, completionTokens: u.CompletionTokens, cacheHitTokens: u.CacheHitTokens, cacheMissTokens: u.CacheMissTokens}
}

func (responsesDialect) carriesContent(body []byte) bool {
	return openairesponses.ResponseCarriesContent(body)
}
func (responsesDialect) stopStated(body []byte) bool {
	return openairesponses.ResponseStatus(body) != ""
}
func (responsesDialect) textBytes(body []byte) int { return openairesponses.ResponseTextBytes(body) }

func (responsesDialect) inspectStreamEvent(payload []byte) nativeStreamEvent {
	info := openairesponses.InspectStreamEvent(payload)
	return nativeStreamEvent{
		eventType:       info.Type,
		terminal:        info.Terminal,
		inputTokens:     info.InputTokens,
		hasInput:        info.HasInput,
		outputTokens:    info.OutputTokens,
		hasOutput:       info.HasOutput,
		cacheHitTokens:  info.CacheHitTokens,
		cacheMissTokens: info.CacheMissTokens,
		errorMessage:    info.ErrorMessage,
		carriesError:    info.CarriesError,
		textBytes:       info.TextBytes,
		sequenceNumber:  info.SequenceNumber,
		hasSequence:     info.HasSequence,
		responseID:      info.ResponseID,
	}
}

func (responsesDialect) streamFailure(message, kind, responseID string, sequence int) []byte {
	return openairesponses.BuildStreamFailure(message, kind, responseID, sequence)
}

// writeError: the Responses API shares the chat error envelope, so the
// gateway's ordinary OpenAI error is already in dialect.
func (responsesDialect) writeError(w http.ResponseWriter, message string, status int) {
	writeOpenAIError(w, message, status)
}
