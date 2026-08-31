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
// The two halves are split by whether a match needs the digit veto below.
//
// ambiguousKeyShape carries prefixes that also start ordinary identifiers and
// prose, so a match with no digit in it is kept: "sk_business_unit_identifier"
// and "Bearer authentication-required" are not credentials.
var ambiguousKeyShape = regexp.MustCompile(`\b(?:sk|gsk|xai|hf|fw|r8)[-_][A-Za-z0-9_-]{16,}|(?i:\bbearer\s+)[A-Za-z0-9._~+/=-]{16,}`)

// unambiguousKeyShape carries prefixes and structures that prose does not
// produce: Google API keys, AWS access key ids, and bare JWTs. These are
// masked whatever they contain, because the digit veto buys nothing here and
// costs real coverage: an AWS access key id is AKIA plus sixteen base32
// characters, and about one in thirty-six of them contains no digit at all.
var unambiguousKeyShape = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}|\bAKIA[A-Z0-9]{16}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}`)

// CredentialMinLen is the shortest provider key the exact-value mask will
// redact. Keyless local providers carry an empty key and a handful of test or
// placeholder setups use tiny ones; rewriting every occurrence of a few
// characters would shred the body without protecting anything. Exported so
// internal/proxy's own masker shares the threshold rather than restating it.
const CredentialMinLen = 8

// MaskKeyShapedTokens scrubs credential-looking substrings from upstream text
// bound for a log, a database column or a client.
//
// A match with no digit in it is an identifier or prose
// ("sk_business_unit_identifier", "Bearer authentication-required") rather
// than a credential, and stays: real keys carry digits. The replacement
// carries no JSON metacharacters, so a valid body stays valid.
//
// Keeping this off model ids matters, because the proxy classifies a
// retirement by finding the requested model's own id beside a phrase in a
// sanitized body, and a scrub that ate an id could change routing. Two things
// hold that line: no model id in the bundled catalogs matches (a test pins
// it), and a replacement cannot CREATE a verdict either, since the brackets in
// "[redacted]" break the phrase-binding gap the classifier requires.
func MaskKeyShapedTokens(body []byte) []byte {
	body = unambiguousKeyShape.ReplaceAll(body, []byte("[redacted]"))
	return ambiguousKeyShape.ReplaceAllFunc(body, func(m []byte) []byte {
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
	if len(secret) >= CredentialMinLen && strings.Contains(body, secret) {
		body = strings.ReplaceAll(body, secret, "[redacted]")
	}
	return string(MaskKeyShapedTokens([]byte(body)))
}
