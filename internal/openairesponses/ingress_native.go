package openairesponses

import (
	"encoding/json"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// The native passthrough: a /v1/responses request routed to OpenAI itself is
// forwarded verbatim and its answer streamed back untouched. The proxy still
// meters it, judges whether it answered, and tells a finished stream from a
// truncated one, from the readings below. The Anthropic passthrough reads the
// same things from its own wire format (anthropic/native.go).

// NativeUsage is the metering summary of one Responses body or stream.
// PromptTokens is the whole prompt; the cache split is reported only when the
// upstream reported cached tokens, and sums back to the prompt when it is.
type NativeUsage struct {
	PromptTokens     int
	CompletionTokens int
	CacheHitTokens   int
	CacheMissTokens  int
}

// ParseResponseUsage reads the usage block of a non-streaming Response for
// metering. A missing or unreadable block yields zeros.
func ParseResponseUsage(body []byte) NativeUsage {
	var resp struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return NativeUsage{}
	}
	return nativeUsageOf(translateUsage(resp.Usage))
}

func nativeUsageOf(u *chatUsage) NativeUsage {
	if u == nil {
		return NativeUsage{}
	}
	out := NativeUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 && u.PromptTokensDetails.CachedTokens <= u.PromptTokens {
		out.CacheHitTokens = u.PromptTokensDetails.CachedTokens
		out.CacheMissTokens = u.PromptTokens - out.CacheHitTokens
	}
	return out
}

// ResponseCarriesContent reports whether a non-streaming Response holds any
// output item. A 200 with an empty output is a status rather than an answer.
func ResponseCarriesContent(body []byte) bool {
	var resp struct {
		Output []json.RawMessage `json:"output"`
	}
	return json.Unmarshal(body, &resp) == nil && len(resp.Output) > 0
}

// ResponseStatus is the status a non-streaming Response states, or "" when it
// states none: the provider saying how its generation ended, the analogue of
// a chat finish_reason and an Anthropic stop_reason. A failed status is not
// an answer, and is reported as "" so a sibling can be asked.
func ResponseStatus(body []byte) string {
	var resp struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Status == "failed" {
		return ""
	}
	return resp.Status
}

// ResponseTextBytes is the byte length of the text, reasoning summary, tool
// name and tool arguments across a Response's output items: the delivered
// output a usage estimate works from when the body carries no usage.
func ResponseTextBytes(body []byte) int {
	var resp Response
	if json.Unmarshal(body, &resp) != nil {
		return 0
	}
	n := 0
	for _, it := range resp.Output {
		for _, c := range it.Content {
			n += len(c.Text)
		}
		for _, s := range it.Summary {
			n += len(s.Text)
		}
		n += len(it.Name) + len(it.Arguments)
	}
	return n
}

// NativeStreamEvent is the decoded summary of one Responses stream event.
type NativeStreamEvent struct {
	Type string
	// Terminal marks the event that ends a Responses stream: completed,
	// incomplete or failed. The stream has no [DONE] sentinel.
	Terminal        bool
	InputTokens     int
	HasInput        bool
	OutputTokens    int
	HasOutput       bool
	CacheHitTokens  int
	CacheMissTokens int
	// ErrorMessage is set on a failed response and on an error event.
	ErrorMessage string
	// CarriesError reports error text on the event, whatever its type, for
	// the credential mask.
	CarriesError bool
	// TextBytes is the byte length of the output a delta event carries.
	TextBytes int
	// SequenceNumber is the event's own, HasSequence whether it carried one;
	// ResponseID is the id the event's response snapshot names, if any. A
	// failure frame the gateway appends continues both.
	SequenceNumber int
	HasSequence    bool
	ResponseID     string
}

// InspectStreamEvent decodes one Responses stream event payload. The usage
// rides on the terminal response snapshot; deltas carry output text. A payload
// that does not parse yields a zero event (Type == "").
func InspectStreamEvent(payload []byte) NativeStreamEvent {
	var ev struct {
		Type           string `json:"type"`
		Delta          string `json:"delta"`
		SequenceNumber *int   `json:"sequence_number"`
		Response       *struct {
			ID    string          `json:"id"`
			Usage json.RawMessage `json:"usage"`
			Error json.RawMessage `json:"error"`
		} `json:"response"`
		// The error event's own members, and a bare error member a relay may
		// stamp on any event.
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(payload, &ev) != nil {
		return NativeStreamEvent{}
	}
	info := NativeStreamEvent{Type: ev.Type, CarriesError: util.ValueCarries(ev.Error)}
	if ev.SequenceNumber != nil {
		info.SequenceNumber, info.HasSequence = *ev.SequenceNumber, true
	}
	if ev.Response != nil {
		info.ResponseID = ev.Response.ID
	}
	switch ev.Type {
	case "response.completed", "response.incomplete", "response.failed":
		info.Terminal = true
		if ev.Response != nil {
			u := nativeUsageOf(translateUsage(ev.Response.Usage))
			// A reading is a positive figure, as on every other passthrough: a
			// zero here would overwrite an earlier count with nothing.
			if u.PromptTokens > 0 {
				info.InputTokens, info.HasInput = u.PromptTokens, true
				info.CacheHitTokens, info.CacheMissTokens = u.CacheHitTokens, u.CacheMissTokens
			}
			if u.CompletionTokens > 0 {
				info.OutputTokens, info.HasOutput = u.CompletionTokens, true
			}
		}
		if ev.Type == "response.failed" {
			info.CarriesError = true
			info.ErrorMessage = "upstream response failed"
			if ev.Response != nil && util.ValueCarries(ev.Response.Error) {
				info.ErrorMessage = util.ErrorMemberMessage(ev.Response.Error)
			}
		}
	case "error":
		info.CarriesError = true
		info.ErrorMessage = ev.Message
		if info.ErrorMessage == "" && util.ValueCarries(ev.Error) {
			info.ErrorMessage = util.ErrorMemberMessage(ev.Error)
		}
		if info.ErrorMessage == "" {
			info.ErrorMessage = "upstream stream error"
		}
	case "response.output_text.delta", "response.function_call_arguments.delta",
		"response.reasoning_summary_text.delta", "response.reasoning_text.delta",
		"response.refusal.delta", "response.custom_tool_call_input.delta":
		info.TextBytes = len(ev.Delta)
	}
	return info
}
