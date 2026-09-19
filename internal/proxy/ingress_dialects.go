package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The two ingress dialects the writer renders: each maps the chat-completions
// bytes the pipeline emits onto its own wire format, stamped with the
// request's own ids.

// --- Anthropic Messages (/v1/messages) ---

type anthropicIngress struct {
	messageID string
	model     string
}

func (anthropicIngress) label() string { return "anthropic" }

func (d anthropicIngress) newStreamTranslator() ingressTranslator {
	return &anthropicIngressStream{tr: anthropic.NewStreamTranslator(d.messageID, d.model), model: d.model}
}

func (d anthropicIngress) buildResponse(chatBody []byte) ([]byte, error) {
	return anthropic.BuildMessageResponse(chatBody, d.messageID, d.model)
}

func (anthropicIngress) buildError(openaiBody []byte, status int) []byte {
	return anthropic.BuildErrorResponse(openaiBody, status)
}

// anthropicIngressStream drives the Anthropic stream translator from raw chunk
// payloads: the decode and its tolerance live here, beside the dialect.
type anthropicIngressStream struct {
	tr    *anthropic.StreamTranslator
	model string
}

func (s *anthropicIngressStream) Translate(payload []byte) ([]byte, error) {
	var chunk anthropic.OAStreamChunk
	// A shape this gateway has no struct for is not broken bytes, and the frame
	// may carry the model's answer: the streaming path forwards payloads
	// verbatim, so a provider's own token-count spelling reaches here, and
	// dropping the frame for one dropped the content riding with it. Same rule
	// handleDataChunk reads, for the same reason.
	// util.DecodeCounts as well as the shape tolerance: this is the OpenAI ->
	// Anthropic translator, so a count the provider spelled differently reaches
	// it verbatim, and keeping the frame while losing the count told the client
	// the model produced zero output tokens for a real answer.
	if err := util.DecodeCounts(payload, &chunk); err != nil && util.ShapeError(payload, err) == nil {
		debuglog.Debug("anthropic: skip unparseable upstream chunk", "error", jsonfault.Describe(err, len(payload)))
		return nil, nil
	}
	return s.tr.Translate(chunk)
}

func (s *anthropicIngressStream) Finish() ([]byte, error) {
	out, err := s.tr.Finish()
	if err != nil {
		return nil, err
	}
	if n := s.tr.LateSignatures(); n > 0 {
		debuglog.Warn("anthropic: thought signatures arrived after their tool_use block opened and could not be carried; the next turn will be refused", "count", n, "model", s.model)
	}
	return out, nil
}

// Fail is the Anthropic `event: error` the SDKs surface as an API error.
func (*anthropicIngressStream) Fail(message, _ string) []byte {
	frame := append([]byte("event: error\ndata: "), anthropic.BuildErrorResponseFromMessage(message, http.StatusBadGateway)...)
	return append(frame, "\n\n"...)
}

// --- OpenAI Responses (/v1/responses) ---

type responsesIngress struct {
	responseID string
	model      string
	facts      *openairesponses.RequestFacts
}

func (responsesIngress) label() string { return "responses" }

func (d responsesIngress) newStreamTranslator() ingressTranslator {
	return openairesponses.NewIngressStreamTranslator(d.responseID, d.model, d.facts)
}

func (d responsesIngress) buildResponse(chatBody []byte) ([]byte, error) {
	return openairesponses.BuildResponse(chatBody, d.responseID, d.model, d.facts)
}

// buildError: the Responses API shares the chat error envelope, so an OpenAI
// envelope is forwarded as it is. A body that is not one (a relay's raw text,
// nothing at all) is wrapped into the envelope the way the chat endpoint
// wraps its own errors, so the client always parses one.
func (responsesIngress) buildError(openaiBody []byte, status int) []byte {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(openaiBody, &env) == nil && util.ValueCarries(env.Error) {
		return openaiBody
	}
	message := string(openaiBody)
	if message == "" {
		message = http.StatusText(status)
	}
	body, err := json.Marshal(map[string]any{"error": map[string]any{
		"message": message,
		"type":    util.OpenAIErrorType(status),
		"code":    status,
	}})
	if err != nil {
		return []byte(`{"error":{"message":"internal error","type":"server_error"}}`)
	}
	return body
}
