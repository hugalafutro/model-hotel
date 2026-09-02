package anthropic

import (
	"encoding/base64"
	"strings"
)

// thoughtSigMarker separates a tool_use id from the Gemini 3 thought
// signature riding on it.
//
// Gemini 3 signs each function call and refuses the follow-up turn without
// the signature. The chat-completions surface carries it on the tool call as
// extra_content.google.thought_signature, Google's own shape, which the SDKs
// keep because they round-trip the call object whole. The Messages surface
// has no such member: a tool_use block is id, name and input, and a native
// Anthropic provider, which the same conversation can fail over to, rejects a
// member it does not know. The id is the one field every Messages client
// echoes back untouched, on the tool_use block and again as the
// tool_result's tool_use_id, so the signature travels inside it, the way
// UniClaudeProxy carries it. The encoding keeps to the id alphabet Anthropic
// accepts ([A-Za-z0-9_-]); an id without the marker, or with a suffix that
// does not decode, is a plain id.
const thoughtSigMarker = "_thoughtsig_"

// signedToolUseID appends signature to id; id alone when unsigned.
func signedToolUseID(id, signature string) string {
	if signature == "" {
		return id
	}
	return id + thoughtSigMarker + base64.RawURLEncoding.EncodeToString([]byte(signature))
}

// splitToolUseID recovers the provider's id and the signature from a signed
// id. The last marker is the one that counts, so an upstream id that happens
// to contain the marker survives.
func splitToolUseID(id string) (string, string) {
	at := strings.LastIndex(id, thoughtSigMarker)
	if at < 0 {
		return id, ""
	}
	sig, err := base64.RawURLEncoding.DecodeString(id[at+len(thoughtSigMarker):])
	if err != nil || len(sig) == 0 {
		return id, ""
	}
	return id[:at], string(sig)
}
