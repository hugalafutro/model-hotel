package util

import (
	"bytes"
	"regexp"
	"strings"
)

// The key-shape patterns match credential-looking substrings a provider may quote
// inside an error body: prefixed secret keys (sk- also covers sk-ant-, sk-or-
// and sk-proj-; hf_, fw_, r8_, gsk_, xai- cover HuggingFace, Fireworks,
// Replicate, Groq and xAI), Google API keys (AIza...), AWS access key ids
// (AKIA...), bare JWTs (the MiniMax API key format), and bearer tokens. The
// minimum tail lengths keep prose like "sk-abc" out of scope, and the digit
// rule below spares prose for the ambiguous half. A prefix list
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
//
// The JWT signature is optional so that header.payload alone still matches.
// Every other pattern here matches its own truncated prefix, but a JWT needed
// BOTH dots, so one cut short — by a truncating caller, or by the scan window
// in SanitizeLogBody — matched nothing and left its head in the output. The
// header and payload are the parts that carry claims; requiring the signature
// to redact them had it backwards.
var unambiguousKeyShape = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}|\bAKIA[A-Z0-9]{16}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}(?:\.[A-Za-z0-9_-]{5,})?`)

// CredentialMinLen is the shortest provider key the exact-value mask will
// redact. Keyless local providers carry an empty key and a handful of test or
// placeholder setups use tiny ones; rewriting every occurrence of a few
// characters would shred the body without protecting anything. Exported so
// internal/proxy's own masker shares the threshold rather than restating it.
const CredentialMinLen = 8

// MaskKeyShapedTokens scrubs credential-looking substrings from upstream text
// bound for a log, a database column or a client.
//
// For the ambiguous half, a match with no digit in it is an identifier or
// prose ("sk_business_unit_identifier", "Bearer authentication-required")
// rather than a credential, and stays: real keys of those shapes carry digits.
// The unambiguous half is masked whatever it contains. The replacement carries
// no JSON metacharacters, so a valid body stays valid.
//
// Keeping this off model ids matters, because the proxy classifies a
// retirement by finding the requested model's own id beside a phrase in a
// sanitized body, and a scrub that ate an id could change routing. Two things
// hold that line: no model id in the bundled catalogs matches (a test pins
// it), and a replacement cannot CREATE a verdict either, since the brackets in
// "[redacted]" break the phrase-binding gap the classifier requires.
func MaskKeyShapedTokens(body []byte) []byte {
	// The ambiguous pass runs FIRST because its alternatives are the outer
	// ones. "Bearer <jwt>" is matched whole by the bearer alternative, but if
	// the JWT is consumed first the bearer alternative no longer has sixteen
	// characters left to match and up to fifteen characters of the token's
	// head survive.
	body = ambiguousKeyShape.ReplaceAllFunc(body, func(m []byte) []byte {
		if !bytes.ContainsAny(m, "0123456789") {
			return m
		}
		return []byte("[redacted]")
	})
	return unambiguousKeyShape.ReplaceAll(body, []byte("[redacted]"))
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
	return MaskCredentials([]string{secret}, body)
}

// MaskCredentials is MaskCredential for a caller that holds more than one
// candidate secret, or none it can name: the exact pass runs for every entry,
// then the shape layer runs once. It exists for the shared discovery helpers,
// which never receive the key as a value, only inside the headers and URL of
// the request they are about to send, and so mask with everything that request
// carries (each bearer, each query value) rather than with one named key.
// Entries shorter than CredentialMinLen are skipped for the reason given there.
func MaskCredentials(secrets []string, body string) string {
	return string(MaskKeyShapedTokens([]byte(maskExact(secrets, body))))
}

// MaskCredentialsBounded is MaskCredentials followed by SanitizeLogBody, in the
// one order that is safe: the exact pass runs BEFORE the truncation, over the
// same maxLen+scrubMargin window SanitizeLogBody scans. Running it after (over
// text already cut at maxLen) leaves the head of a key that straddles the cut,
// and a custom-format key gets nothing from the shape pass behind it either.
// This is the same rule SanitizeLogBody documents for its own shape pass; a
// caller that has a body to bound and secrets to name uses this, not the two
// in sequence.
func MaskCredentialsBounded(secrets []string, body string, maxLen int) string {
	if len(body) > maxLen+scrubMargin {
		body = body[:maxLen+scrubMargin]
	}
	out := SanitizeLogBody(maskExact(secrets, body), maxLen)
	// The window cut above can leave the head of a secret at its very end, and
	// masking SHRINKS the text ("[redacted]" is shorter than a key), so enough
	// earlier occurrences pull that cut head down below maxLen where the final
	// truncation no longer removes it. Nothing before this point can know how
	// far the text moved, so the tail is checked last: a proper prefix of any
	// listed secret, of credential length, is redacted too. Only the tail can
	// hold one, since a whole occurrence anywhere was already replaced.
	suffix := ""
	if strings.HasSuffix(out, "…") {
		out, suffix = strings.TrimSuffix(out, "…"), "…"
	}
	for _, secret := range secrets {
		if len(secret) < CredentialMinLen {
			continue
		}
		for k := min(len(secret)-1, len(out)); k >= CredentialMinLen; k-- {
			if strings.HasSuffix(out, secret[:k]) {
				out = out[:len(out)-k] + "[redacted]"
				break
			}
		}
	}
	return out + suffix
}

// MaskCredentialBounded is MaskCredentialsBounded for a caller that holds one
// key. It is the form every vendor path that logs or returns an upstream body
// uses; MaskCredential over an already-sanitized body is the inverted order
// and must not be written.
func MaskCredentialBounded(secret, body string, maxLen int) string {
	return MaskCredentialsBounded([]string{secret}, body, maxLen)
}

// maskExact replaces every listed secret of credential length. It runs the
// list in order, so a caller that lists a superset ("Bearer X") before its
// subset ("X") has the whole token consumed first.
func maskExact(secrets []string, body string) string {
	for _, secret := range secrets {
		if len(secret) >= CredentialMinLen && strings.Contains(body, secret) {
			body = strings.ReplaceAll(body, secret, "[redacted]")
		}
	}
	return body
}
