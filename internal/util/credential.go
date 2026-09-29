package util

import (
	"bytes"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// The key-shape patterns match credential-looking substrings a provider may quote
// inside an error body: prefixed secret keys (sk- also covers sk-ant-, sk-or-
// and sk-proj-; hf_, fw_, r8_, gsk_, xai- cover HuggingFace, Fireworks,
// Replicate, Groq and xAI), Google API keys (AIza...), AWS access key ids
// (AKIA...), bare JWTs (the MiniMax API key format), and bearer tokens. The
// minimum tail lengths keep prose like "sk-abc" out of scope, and the digit
// rule below spares prose for the ambiguous half. A prefix list necessarily
// trails the provider roster, so it is never the only layer: MaskCredential
// runs an exact match of the gateway's own key in front of it, and the proxy
// gates it behind a status class.
//
// It lives here rather than in internal/proxy because the proxy is not the
// only thing that handles upstream error text: the dashboard's model test and
// provider discovery decrypt the same credential and write the same bodies to
// the same tables. The two halves are split by whether a match needs the digit
// veto below.
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
// Every other pattern here matches its own truncated prefix; requiring BOTH
// dots would leave the head of a JWT cut short (by a truncating caller, or by
// the scan window in SanitizeLogBody) in the output, and the header and
// payload are the parts that carry claims.
var unambiguousKeyShape = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}|\bAKIA[A-Z0-9]{16}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}(?:\.[A-Za-z0-9_-]{5,})?`)

// secretParamShape is a credential passed by name in a query string or a form
// body ("?api_key=...", "&client_secret=..."), whatever format the value has:
// the name says what it is, so no key shape is needed. The name must start a
// parameter (the text start, "?", "&", whitespace, a quote, ":", an opening
// bracket, "," or ";") and be one of the credential names, so "max_token=5",
// "has_secret=true" and "prompt_token=3" are left alone. A bare "key=" is not
// one: the gateway logs a virtual key's NAME under that attribute. The value
// stops at the next separator (& , ;), a closing bracket, whitespace, a quote
// or a backslash, so the rest of the line survives and a JSON body stays
// valid.
var secretParamShape = regexp.MustCompile(`(?i)(^|[?&\s"':(\[{,;])((?:client_|refresh_|access_|auth_)?(?:token|secret|password)|(?:api|access|secret|client)[_-]?key)=[^&,;)\]}\s"'\\<>]+`)

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
// hold that line: no model id in the bundled catalogs matches, and a
// replacement cannot CREATE a verdict either, since the brackets in
// "[redacted]" break the phrase-binding gap the classifier requires.
//
// A URL's userinfo (scheme://user:password@host) and a credential passed by
// name as a query or form parameter are masked by the same pass: neither needs
// a recognisable key format, and an unheld custom token reaches a log that way.
// The userinfo rule is URLUserinfoRE, the one RedactURLUserinfo uses; the
// scheme and host stay readable.
func MaskKeyShapedTokens(body []byte) []byte {
	// The ambiguous pass runs FIRST because its alternatives are the outer
	// ones. "Bearer <jwt>" is matched whole by the bearer alternative; with
	// the JWT consumed first, that alternative no longer has sixteen
	// characters left to match and up to fifteen characters of the token's
	// head survive.
	body = ambiguousKeyShape.ReplaceAllFunc(body, func(m []byte) []byte {
		if !bytes.ContainsAny(m, "0123456789") {
			return m
		}
		return []byte("[redacted]")
	})
	body = unambiguousKeyShape.ReplaceAll(body, []byte("[redacted]"))
	body = URLUserinfoRE.ReplaceAll(body, []byte("${1}[redacted]@"))
	// A value already masked matches again up to its closing bracket; kept as
	// is, so that masking twice does not append another "]". Only a complete
	// marker is kept: a bare "[redacted" with no "]" after it is a value.
	var out []byte
	last := 0
	for _, loc := range secretParamShape.FindAllIndex(body, -1) {
		start, end := loc[0], loc[1]
		eq := start + bytes.IndexByte(body[start:end], '=') + 1
		if string(body[eq:end]) == "[redacted" && end < len(body) && body[end] == ']' {
			continue
		}
		out = append(append(out, body[last:eq]...), "[redacted]"...)
		last = end
	}
	if out == nil {
		return body
	}
	return append(out, body[last:]...)
}

// mayHoldShape is a literal check every shape pattern implies: the prefixed
// keys need "-" or "_", a bearer token the word "bearer", a parameter "=",
// URL userinfo "://", and the rest their fixed prefixes. Text without any of
// them skips the regexp scans, which on a multi-megabyte prompt cost seconds.
func mayHoldShape(s string) bool {
	if strings.ContainsAny(s, "-_=") || strings.Contains(s, "://") || strings.Contains(s, "AIza") ||
		strings.Contains(s, "AKIA") || strings.Contains(s, "eyJ") {
		return true
	}
	for i := 0; i+len("bearer") <= len(s); i++ {
		if (s[i] == 'b' || s[i] == 'B') && strings.EqualFold(s[i:i+len("bearer")], "bearer") {
			return true
		}
	}
	return false
}

// maskShapes is MaskKeyShapedTokens over a string. Text no pattern matches,
// nearly every log line, is returned as is: no copy, no replacement pass.
func maskShapes(s string) string {
	if !mayHoldShape(s) {
		return s
	}
	if !ambiguousKeyShape.MatchString(s) && !unambiguousKeyShape.MatchString(s) &&
		!URLUserinfoRE.MatchString(s) && !secretParamShape.MatchString(s) {
		return s
	}
	return string(MaskKeyShapedTokens([]byte(s)))
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
// Callers hold a decrypted key only while they are talking to an upstream, and
// this is the function to run over anything that upstream says back.
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
// Every exact pass also masks the held set (held_secrets.go): the caller names
// only the secrets it knows about, and a relay can quote any other.
func MaskCredentials(secrets []string, body string) string {
	return maskShapes(MaskExactCredentials(secrets, body))
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
	return stripSecretTail(sanitizeShape(MaskExactCredentials(secrets, body), maxLen, nil), secrets, maxLen)
}

// stripSecretTail redacts a proper prefix of any listed or held secret, of
// credential length, left at the very end of out. The scan window cut can
// leave the head of a secret at its very end, and masking SHRINKS the text
// ("[redacted]" is shorter than a key), so enough earlier replacements pull
// that cut head down below maxLen where the final truncation no longer
// removes it. Nothing before this point can know how far the text moved, so
// the tail is checked last. Only the tail can hold one, since a whole
// occurrence anywhere was already replaced or left whole on purpose.
func stripSecretTail(out string, secrets []string, maxLen int) string {
	suffix := ""
	if strings.HasSuffix(out, "…") {
		out, suffix = strings.TrimSuffix(out, "…"), "…"
	}
	// The longest matching head across ALL secrets, stripped once. Taking the
	// first secret that matches and stopping would let a second secret's head
	// survive whenever an earlier secret's prefix is a suffix of it.
	longest := 0
	for _, secret := range withHeld(secrets) {
		if len(secret) < CredentialMinLen {
			continue
		}
		for k := min(len(secret)-1, len(out)); k > longest && k >= CredentialMinLen; k-- {
			if strings.HasSuffix(out, secret[:k]) {
				longest = k
				break
			}
		}
	}
	if longest > 0 {
		out = out[:len(out)-longest] + "[redacted]"
		// "[redacted]" is ten bytes, so a head of eight or nine grew the text
		// past the bound this function promises. Cut back and say so.
		if len(out) > maxLen {
			out, suffix = out[:maxLen], "…"
		}
	}
	return out + suffix
}

// MaskCredentialBounded is MaskCredentialsBounded for a caller that holds one
// key. It is the form every vendor path that logs or returns an upstream body
// uses; MaskCredential over an already-sanitized body is the inverted order
// and unsafe.
func MaskCredentialBounded(secret, body string, maxLen int) string {
	return MaskCredentialsBounded([]string{secret}, body, maxLen)
}

// MaskExactCredentials is the exact pass alone: every listed secret of
// credential length and every held secret (see withHeld for the order), each
// also in its URL-escaped forms (escapedForms), so a key quoted inside a
// logged URL ("+" as "%2B", "/" as "%2F", in a query or a path segment) is
// masked too. A held secret's forms are held beside it (HoldSecret); a listed
// one's are built here. It cannot false-positive, so it is the pass for error
// text that must not meet the shape regexes (prose a client reads). A caller
// that lists a superset ("Bearer X") before its subset ("X") has the whole
// token consumed first.
func MaskExactCredentials(secrets []string, body string) string {
	for _, secret := range withHeld(secrets) {
		body = maskOne(secret, body)
	}
	for _, secret := range secrets {
		if len(secret) >= CredentialMinLen {
			for _, form := range escapedForms(secret) {
				body = maskOne(form, body)
			}
		}
	}
	return body
}

func maskOne(secret, body string) string {
	if len(secret) >= CredentialMinLen && strings.Contains(body, secret) {
		return strings.ReplaceAll(body, secret, "[redacted]")
	}
	return body
}

// escapedForms returns the renderings of secret a URL can carry that differ
// from it: query-escaped and path-escaped, each also with lowercase hex
// ("%2f"), which some encoders emit. Mixed-case hex within one key is not
// covered.
func escapedForms(secret string) []string {
	var forms []string
	for _, f := range []string{url.QueryEscape(secret), url.PathEscape(secret)} {
		if f == secret {
			continue
		}
		lower := percentHex.ReplaceAllStringFunc(f, strings.ToLower)
		for _, g := range []string{f, lower} {
			if !slices.Contains(forms, g) {
				forms = append(forms, g)
			}
		}
	}
	return forms
}

var percentHex = regexp.MustCompile(`%[0-9A-F]{2}`)
