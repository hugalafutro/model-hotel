package util

import (
	"bytes"
	"regexp"
	"strings"
)

// keyShapedToken matches credential-looking substrings a provider may quote
// inside an error body: prefixed secret keys (sk- also covers sk-ant-, sk-or-
// and sk-proj-; hf_, fw_, r8_, gsk_, xai- cover HuggingFace, Fireworks,
// Replicate, Groq and xAI), Google API keys (AIza...), AWS access key ids
// (AKIA...), bare JWTs (the MiniMax API key format), and bearer tokens. The
// minimum tail lengths keep prose like "sk-abc" out of scope; matches without
// a digit are prose too and are dropped by MaskKeyShapedTokens. A prefix list
// necessarily trails the provider roster, so it is never the only layer:
// MaskCredential runs an exact match of the gateway's own key in front of it,
// and the proxy gates it behind a status class.
//
// It lives here rather than in internal/proxy because the proxy is not the
// only thing that handles upstream error text. The dashboard's model test and
// provider discovery decrypt the same credential and write the same bodies to
// the same tables, and when this rule lived in the proxy alone those paths had
// only a UUID scrub, so an upstream quoting the key back ("Incorrect API key
// provided: sk-...") put it in request_logs and app_logs in cleartext.
var keyShapedToken = regexp.MustCompile(`\b(?:sk|gsk|xai|hf|fw|r8)[-_][A-Za-z0-9_-]{16,}|\bAIza[0-9A-Za-z_-]{30,}|\bAKIA[0-9A-Z]{16}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}|(?i:\bbearer\s+)[A-Za-z0-9._~+/=-]{16,}`)

// credentialMinLen is the shortest provider key the exact-value mask will
// redact. Keyless local providers carry an empty key and a handful of test or
// placeholder setups use tiny ones; rewriting every occurrence of a few
// characters would shred the body without protecting anything.
const credentialMinLen = 8

// MaskKeyShapedTokens scrubs credential-looking substrings from upstream text
// bound for a log, a database column or a client.
//
// A match with no digit in it is an identifier or prose
// ("sk_business_unit_identifier", "Bearer authentication-required") rather
// than a credential, and stays: real keys carry digits. The replacement
// carries no JSON metacharacters, so a valid body stays valid.
//
// The digit rule is also what keeps this off model ids, which share the
// prefix vocabulary: none of the 1,033 model ids on the live fleet, and none
// in the bundled catalogs, matches. That matters because the proxy classifies
// a retirement by finding the requested model's own id beside a phrase in a
// sanitized body, and a scrub that ate the id would silently change routing.
func MaskKeyShapedTokens(body []byte) []byte {
	if !bytes.ContainsAny(body, "0123456789") {
		return body
	}
	return keyShapedToken.ReplaceAllFunc(body, func(m []byte) []byte {
		if !bytes.ContainsAny(m, "0123456789") {
			return m
		}
		return []byte("[redacted]")
	})
}

// MaskCredential scrubs one provider's credential out of text bound for a log,
// a database column or a dashboard response: first the exact key, then any
// key-shaped token.
//
// The exact pass runs first and cannot false-positive, so it covers every key
// shape including the custom and self-hosted gateways the prefix regex can
// never anticipate. It does not cover a JSON-escaped rendering of the key (an
// encoder turning "&" into "\u0026" defeats it; real keys rarely carry such
// bytes), which is what the shape layer behind it is for.
//
// This is the same two-layer rule internal/proxy applies to the bodies it logs
// and forwards. Callers outside that package hold a decrypted key only while
// they are talking to an upstream, and this is the function to run over
// anything that upstream says back.
func MaskCredential(secret, body string) string {
	if len(secret) >= credentialMinLen && strings.Contains(body, secret) {
		body = strings.ReplaceAll(body, secret, "[redacted]")
	}
	return string(MaskKeyShapedTokens([]byte(body)))
}
