package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// thoughtSigMarker separates a tool_use id from the Gemini 3 thought
// signature riding on it.
//
// Gemini 3 signs each function call and refuses the follow-up turn without
// the signature. The chat-completions surface carries it on the tool call as
// extra_content.google.thought_signature; the Messages surface has no such
// member, and a native Anthropic provider rejects a member it does not know.
// The id is the one field every Messages SDK echoes back untouched, on the
// tool_use block and again as the tool_result's tool_use_id, so the signature
// travels inside it. The encoding keeps to the id alphabet Anthropic accepts
// ([A-Za-z0-9_-]); an id without the marker, or with a suffix that does not
// decode, is a plain id.
//
// The signature is base64 text on Google's wire. Carrying it as the bytes it
// encodes (tag "b", put back through the same encoding on the way in) rather
// than as text (tag "t", for a signature that is not padded base64) keeps the
// id a third shorter, and an id is echoed twice per call per turn. The tag is
// also the form's version: a payload under a tag this build does not know is a
// plain id, so a changed form degrades to Gemini's refusal of the turn, never
// to a corrupted signature.
const thoughtSigMarker = "_thoughtsig_"

const (
	sigTagBytes = 'b'
	sigTagText  = 't'
)

// signedToolUseID appends signature to id; id alone when unsigned.
func signedToolUseID(id, signature string) string {
	if signature == "" {
		return id
	}
	// Re-encoding proves the bytes give the signature back exactly: the
	// decoder skips line breaks and accepts non-canonical trailing bits,
	// either of which would round-trip to a different string.
	if raw, err := base64.StdEncoding.DecodeString(signature); err == nil && base64.StdEncoding.EncodeToString(raw) == signature {
		return id + thoughtSigMarker + string(sigTagBytes) + base64.RawURLEncoding.EncodeToString(raw)
	}
	return id + thoughtSigMarker + string(sigTagText) + base64.RawURLEncoding.EncodeToString([]byte(signature))
}

// splitToolUseID recovers the provider's id and the signature from a signed
// id. The last marker is the one that counts, so an upstream id that happens
// to contain the marker survives. The base64 alphabet can spell the marker,
// so a payload containing it is possible but vanishingly unlikely; the tag
// and decode checks then fall back to the plain id, as does any payload that
// does not decode or carries an unknown tag.
func splitToolUseID(id string) (string, string) {
	at := strings.LastIndex(id, thoughtSigMarker)
	if at < 0 {
		return id, ""
	}
	payload := id[at+len(thoughtSigMarker):]
	if len(payload) < 2 {
		return id, ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload[1:])
	if err != nil || len(raw) == 0 {
		return id, ""
	}
	switch payload[0] {
	case sigTagBytes:
		return id[:at], base64.StdEncoding.EncodeToString(raw)
	case sigTagText:
		return id[:at], string(raw)
	}
	return id, ""
}

// StripSignedToolUseIDs returns a Messages body with the signature suffix
// removed from every tool_use id and tool_result tool_use_id, for the native
// passthrough: an Anthropic provider has no use for a Gemini signature, and
// the ids stay paired since both ends are stripped alike. A body that is not
// the expected shape, or that carries no signed id, is returned as it came.
func StripSignedToolUseIDs(body []byte) []byte {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return body
	}
	var messages []json.RawMessage
	if json.Unmarshal(top["messages"], &messages) != nil {
		return body
	}
	changed := false
	for i, raw := range messages {
		var msg map[string]json.RawMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(msg["content"], &blocks) != nil {
			continue
		}
		blockChanged := false
		for _, b := range blocks {
			for _, key := range []string{"id", "tool_use_id"} {
				var id string
				if json.Unmarshal(b[key], &id) != nil || !strings.Contains(id, thoughtSigMarker) {
					continue
				}
				bare, sig := splitToolUseID(id)
				if sig == "" {
					continue
				}
				b[key], _ = json.Marshal(bare)
				blockChanged = true
			}
		}
		if !blockChanged {
			continue
		}
		msg["content"], _ = json.Marshal(blocks)
		messages[i], _ = json.Marshal(msg)
		changed = true
	}
	if !changed {
		return body
	}
	top["messages"], _ = json.Marshal(messages)
	out, err := json.Marshal(top)
	if err != nil {
		return body
	}
	return out
}
