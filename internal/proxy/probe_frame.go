package proxy

// What one SSE data payload means to the first-token probe: the error
// envelope, the terminators, and the reading of "did the model say something"
// that decides when a stream is committed to the client. probeFirstToken in
// proxy.go drives these.

import (
	"encoding/json"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// errorEnvelopeMessage reports the provider's own message when an SSE data frame
// is an error envelope instead of a token, and ok == false for every ordinary
// frame.
//
// Whether the frame is an error is util.ValueCarries' decision alone (a
// populated error member of any shape, including Ollama's bare string; not
// null/{}/""/[]/false/0, which leave a caller nothing to read). Deciding it a
// second time here is how the two drift, and either direction is a bug: a miss
// lets a broken provider win a hedged race, a false positive fails over a
// healthy stream.
//
// Only the message is extracted here, by util.ErrorMemberMessage, which renders
// shapes wider than {"error":{"message":...}}.
func errorEnvelopeMessage(content string) (msg string, ok bool) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return "", false
	}
	raw := envelope["error"]
	if !util.ValueCarries(raw) {
		return "", false
	}
	// A bare string, a list, a number, or an object without a "message": render
	// what the provider put there rather than dropping a frame already judged to
	// be an error. Bounded by the caller's sanitizer.
	return util.ErrorMemberMessage(raw), true
}

// probeFrame is what one SSE data payload means to the first-token probe.
type probeFrame int

const (
	// probeFrameNotAToken is a data line carrying no output: an empty or
	// whitespace-only field, or a frame frameCarriesOutput rejects (a
	// role-only opener, a usage-only chunk, an Anthropic message_start or
	// ping). Skipped exactly like a keepalive comment: it is not a token, but
	// it is not a verdict either, so a real frame after it wins.
	//
	// Counting such a frame as a token would commit the stream to the client
	// on a role opener, and a provider that then sends keepalives for minutes
	// without a token holds the client until the client's own timeout, past
	// every failover this gateway could have made. The probe's caller still
	// remembers that a frame was seen: a stream that ENDS behind one is an
	// empty answer and commits (see probeFirstToken), only a stream that
	// stays open behind one times out.
	probeFrameNotAToken probeFrame = iota
	// probeFrameToken is a real first token: the provider is answering.
	probeFrameToken
	// probeFrameEmptyStream is the [DONE] terminator. Whether it means an
	// empty stream or an empty answer depends on what came before it, which
	// the caller knows and this classifier does not.
	probeFrameEmptyStream
	// probeFrameError is an error envelope: the provider reported its failure.
	probeFrameError
)

// classifyProbeFrame decides what a "data:" payload tells the probe. content is
// expected already trimmed. The returned message is the provider's own text, and
// is only populated for probeFrameError.
//
// One classifier, used by both the main scanner loop and the scanner-error
// recovery branch, so the two cannot drift.
func classifyProbeFrame(content string) (probeFrame, string) {
	switch content {
	case "":
		return probeFrameNotAToken, ""
	case "[DONE]":
		return probeFrameEmptyStream, ""
	}
	if msg, isErr := errorEnvelopeMessage(content); isErr {
		return probeFrameError, msg
	}
	if isAnthropicMessageStop(content) {
		// The native stream's terminator, which carries no [DONE]: the
		// caller reads it like one, an empty answer behind frames and an
		// empty stream with none. Without this a relay that holds the body
		// open after message_stop would run an empty answer into the timeout.
		return probeFrameEmptyStream, ""
	}
	if !frameCarriesOutput(content) {
		return probeFrameNotAToken, ""
	}
	return probeFrameToken, ""
}

// isAnthropicMessageStop reports whether a payload is the native Anthropic
// stream's terminal event.
func isAnthropicMessageStop(payload string) bool {
	var ev struct {
		Type string `json:"type"`
	}
	return json.Unmarshal([]byte(payload), &ev) == nil && ev.Type == "message_stop"
}

// chunkMetadata are the members of an OpenAI-shaped chunk that describe the
// chunk rather than carry output. A frame with no choices whose members are
// all of these (a relay's metadata opener, a usage-only frame spelled
// without the choices member) carries nothing.
var chunkMetadata = map[string]bool{"id": true, "object": true, "created": true, "model": true, "system_fingerprint": true, "service_tier": true, "usage": true, "choices": true}

// outputMembers are the delta members that carry model output on the OpenAI
// chunk shape: text, the three spellings of reasoning, tool and function
// calls, a generated image, audio, and a refusal.
var outputMembers = []string{"content", "reasoning_content", "reasoning", "reasoning_details", "tool_calls", "function_call", "images", "audio", "refusal"}

// frameCarriesOutput reports whether a "data:" payload carries model output,
// which is what the TTFT probe waits for before committing a stream to the
// client.
//
// On the OpenAI chunk shape a frame carries output when any choice's delta
// carries one of outputMembers, or the choice carries a legacy completion
// text; a role-only opener, an empty choices list and a usage-only chunk do
// not. On the Anthropic event shape a content block start or delta with
// text, thinking, tool input or a tool name carries output; message_start,
// ping, an empty text block opener, block stops and message_delta/stop do
// not. Presence is read with outputCarries, not util.ValueCarries: a
// whitespace-only token is output where an all-whitespace error member is
// not.
//
// A payload of a shape this gateway does not model (not JSON, no choices
// member, a delta that is not an object, an event type it does not know)
// counts as output: an unknown dialect must never be cut for being unknown.
// Without this reading a relay that answers 200, sends a role opener and
// then keepalives every few seconds while its model produces nothing looks
// alive to the probe, and the client waits on it until its own timeout.
func frameCarriesOutput(payload string) bool {
	var frame struct {
		Type    string                       `json:"type"`
		Choices []map[string]json.RawMessage `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &frame) != nil {
		return true
	}
	switch frame.Type {
	case "content_block_start", "content_block_delta":
		return anthropic.InspectStreamEvent([]byte(payload)).TextBytes > 0
	case "message_start", "ping", "message_delta", "message_stop", "content_block_stop":
		return false
	}
	if frame.Choices == nil {
		// No choices: the same nothing as "choices":[] when every member is
		// chunk metadata (a relay's opener, a usage-only frame spelled
		// without the choices member), otherwise a shape this gateway does
		// not model.
		var members map[string]json.RawMessage
		_ = json.Unmarshal([]byte(payload), &members)
		for k := range members {
			if !chunkMetadata[k] {
				return true
			}
		}
		return false
	}
	for _, choice := range frame.Choices {
		rawDelta, hasDelta := choice["delta"]
		if !hasDelta {
			if text, ok := choice["text"]; ok {
				var v any
				_ = json.Unmarshal(text, &v)
				if outputCarries(v) {
					return true
				}
				continue
			}
			// Neither a delta nor a legacy text. A terminal chunk spelled
			// without a delta member carries only its bookkeeping; a choice
			// with any other member is a shape this gateway does not model.
			for k := range choice {
				switch k {
				case "index", "finish_reason", "native_finish_reason", "logprobs":
				default:
					return true
				}
			}
			continue
		}
		var delta map[string]any
		if err := json.Unmarshal(rawDelta, &delta); err != nil {
			// A delta that is not an object: unmodelled, so never cut.
			return true
		}
		for _, member := range outputMembers {
			if outputCarries(delta[member]) {
				return true
			}
		}
	}
	return false
}

// outputCarries reports whether a decoded delta member holds model output. It
// reads like util.valueCarries with two differences that matter for tokens
// rather than error members: a string counts by presence, not by trimmed
// length, since a whitespace-only token (a leading newline, indentation in
// generated code) is ordinary output; and the keys that only label a part
// (type, index, id) never make a part output on their own, so a
// content-as-parts opener ({"type":"text","text":""}) and a tool-call header
// with no name yet carry nothing.
func outputCarries(v any) bool {
	switch v := v.(type) {
	case string:
		return v != ""
	case map[string]any:
		for k, val := range v {
			if k == "type" || k == "index" || k == "id" {
				continue
			}
			if outputCarries(val) {
				return true
			}
		}
		return false
	case []any:
		for _, val := range v {
			if outputCarries(val) {
				return true
			}
		}
		return false
	case nil, bool, float64:
		return false
	default:
		return true
	}
}

// recoverProbeFrame finds the first complete, meaningful SSE data line in a
// probe buffer and classifies it. found is false when the buffer holds none.
func recoverProbeFrame(bufStr string) (verdict probeFrame, msg string, found bool) {
	sawFrame := false
	for rawLine := range strings.SplitSeq(bufStr, "\n") {
		l := strings.TrimSpace(rawLine)
		content, isData := strings.CutPrefix(l, "data:")
		if !isData {
			continue
		}
		// Reject partial lines: a complete SSE line must be followed by \n in
		// the buffer. Without this guard a mid-line network fragment like
		// "data: hel" (no \n) would pass HasPrefix but represent malformed data.
		if !strings.Contains(bufStr, rawLine+"\n") {
			continue
		}
		// Same classifier as the main loop, so a frame recovered from the buffer
		// is judged exactly as one read straight off the scanner.
		content = strings.TrimSpace(content)
		v, m := classifyProbeFrame(content)
		if v == probeFrameNotAToken {
			// Carries nothing; keep looking for a frame that does.
			sawFrame = sawFrame || content != ""
			continue
		}
		if v == probeFrameEmptyStream && sawFrame {
			// Same reading as the main loop: a terminator behind a frame is an
			// empty answer, which commits.
			v = probeFrameToken
		}
		return v, m, true
	}
	return probeFrameNotAToken, "", false
}
