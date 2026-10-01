package provider

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// credentialHeaders are the request headers a discovery family folds its key
// into. secretsOf reads the key back off these, so the shared helpers can
// scrub what an upstream says back without ever being handed the key as a
// value. A family that authenticates through another header must add it here.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key"}

// credentialQueryParams are the query parameter names a key may travel in
// (a custom gateway may authenticate by ?key=). Only these are treated as
// secrets: a sweep of every query value would redact Azure's ?api-version=...
// out of the one diagnostic an operator needs when a version is refused.
var credentialQueryParams = map[string]bool{
	"key": true, "api_key": true, "apikey": true, "api-key": true,
	"token": true, "access_token": true, "secret": true, "password": true,
}

// secretsOf collects every credential the given headers and URL carry, so text
// the upstream sends back (or a transport error that quotes the URL) can be
// scrubbed of it exactly, before the shape layer. The helpers below never
// receive the key as a value: the caller has already folded it into a header
// or the query string. Reading it back is what lets the shared path cover
// every family, including the custom and self-hosted gateways whose key
// format no prefix regex anticipates.
//
// A header value is listed raw and, for a bearer, stripped, since an upstream
// may quote either. A query value is listed decoded, percent-escaped, and as
// its raw "name=value" segment, because a url.Error quotes the URL exactly as
// it was sent while Query() hands back the decoded form. Short values fall
// out inside the mask.
func secretsOf(h http.Header, u *url.URL) []string {
	var secrets []string
	for _, name := range credentialHeaders {
		for _, v := range h.Values(name) {
			secrets = append(secrets, v)
			if f := strings.Fields(v); len(f) == 2 && strings.EqualFold(f[0], "bearer") {
				secrets = append(secrets, f[1])
			}
		}
	}
	if u == nil {
		return secrets
	}
	return append(secrets, querySecrets(u.RawQuery)...)
}

// querySecrets is the query half of secretsOf, over a raw query string, so a
// URL that failed to parse (and whose error therefore prints it whole) can
// still be scrubbed from the text after its '?'.
func querySecrets(rawQuery string) []string {
	var secrets []string
	for _, seg := range strings.Split(rawQuery, "&") {
		name, raw, ok := strings.Cut(seg, "=")
		if !ok || !credentialQueryParams[strings.ToLower(name)] {
			continue
		}
		secrets = append(secrets, seg, raw)
		if dec, err := url.QueryUnescape(raw); err == nil && dec != raw {
			secrets = append(secrets, dec, url.QueryEscape(dec))
		}
		// A key pasted with a stray control byte (a newline at the end, a
		// wrap in the middle, a DEL) is what makes a URL unparseable in the
		// first place, and url.Error renders the URL with %q, so that byte
		// shows escaped and the exact match on the raw value misses the
		// visible text. The %q rendering IS the visible text, so it is listed
		// too, for a control byte anywhere in the value.
		for _, v := range []string{seg, raw} {
			if q := strconv.Quote(v); q[1:len(q)-1] != v {
				secrets = append(secrets, q[1:len(q)-1])
			}
		}
	}
	return secrets
}

// rawURLQuerySecrets is querySecrets for a URL that may not parse: the query
// is split off by hand, since url.Parse fails on the same input.
func rawURLQuerySecrets(rawURL string) []string {
	_, q, ok := strings.Cut(rawURL, "?")
	if !ok {
		return nil
	}
	return querySecrets(q)
}

// maskRawURLText scrubs text that quotes a raw, possibly unparseable URL: the
// userinfo by pattern, since no parsed URL hands it back, then the listed
// secrets and anything key-shaped, bounded to 500 runes.
func maskRawURLText(secrets []string, text string) string {
	return util.MaskCredentialsBounded(secrets, util.RedactURLUserinfo(text), 500)
}

// requestSecrets is secretsOf for a built request.
func requestSecrets(req *http.Request) []string {
	if req == nil {
		return nil
	}
	return secretsOf(req.Header, req.URL)
}

// maskRequestSecrets scrubs text of everything req carried, then of anything
// key-shaped, then bounds it: in that order, so a key straddling the cut is
// still redacted whole. Used for every upstream body or transport error the
// shared helpers turn into an error or a log line.
func maskRequestSecrets(req *http.Request, text string, maxLen int) string {
	return util.MaskCredentialsBounded(requestSecrets(req), text, maxLen)
}

// maskedError is a transport error whose text has been scrubbed of what the
// request carried, with the original still reachable through Unwrap. So
// errors.Is on a cancelled context or a deadline, and errors.As on a net.Error,
// keep working for every caller, while nothing that prints the error (%v, %s,
// slog, a stored column) can reach the unscrubbed text: those all go through
// Error(). The masked text is computed once, at construction.
type maskedError struct {
	text  string
	cause error
}

func (e *maskedError) Error() string { return e.text }
func (e *maskedError) Unwrap() error { return e.cause }

// maskedRequestError wraps err so its text is scrubbed of everything req
// carried (a url.Error quotes the request URL, and a custom gateway may
// authenticate by query parameter) and of anything key-shaped, bounded to 500
// runes.
func maskedRequestError(req *http.Request, err error) error {
	if err == nil {
		return nil
	}
	return &maskedError{text: maskRequestSecrets(req, err.Error(), 500), cause: err}
}
