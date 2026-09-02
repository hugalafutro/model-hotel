package egress

import "encoding/json"

// ExtraContent is the extra_content member of an OpenAI-shaped tool call,
// Google's carrier for a Gemini 3 thought signature on that wire: the model
// signs each function call and refuses the follow-up turn without the
// signature, so it travels out to the client and back. The chat path keeps the
// member verbatim; the dialect translators read and write it here so they
// agree on the shape.
type ExtraContent struct {
	Google *GoogleExtraContent `json:"google,omitempty"`
}

// GoogleExtraContent is the google member of ExtraContent.
type GoogleExtraContent struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// ThoughtSignatureIn reads the signature out of a raw extra_content member.
// Lenient on purpose: a member of an unexpected shape is an unsigned call,
// never a failed request.
func ThoughtSignatureIn(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var e ExtraContent
	if json.Unmarshal(raw, &e) != nil || e.Google == nil {
		return ""
	}
	return e.Google.ThoughtSignature
}

// ExtraContentFor wraps a signature for the wire; nil for an unsigned call,
// so the member is absent rather than empty.
func ExtraContentFor(signature string) *ExtraContent {
	if signature == "" {
		return nil
	}
	return &ExtraContent{Google: &GoogleExtraContent{ThoughtSignature: signature}}
}
