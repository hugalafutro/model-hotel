package util

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// SanitizeBaseURL removes trailing slashes from a base URL.
func SanitizeBaseURL(raw string) string {
	return strings.TrimSuffix(raw, "/")
}

// SanitizeAPIURL sanitizes a provider base URL for API use.
// It removes trailing slashes and "/v1" suffix, which is the common pattern
// when constructing API endpoints from provider base URLs.
func SanitizeAPIURL(baseURL string) string {
	clean := SanitizeBaseURL(baseURL)
	return strings.TrimSuffix(clean, "/v1")
}

// SplitAndTrim splits a string by comma, trims whitespace from each element,
// and filters out empty strings. Returns nil if the input is empty.
func SplitAndTrim(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	var result []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// URLUserinfoRE matches the userinfo of a URL rendered inside free text:
// everything between "scheme://" and the last @ before the host. Greedy on
// purpose, since a percent-decoded email-style username carries its own @ and
// only the last one separates it from the host. Group 1 is the scheme prefix,
// so a caller can keep or drop the "@". Only the scheme form is recognised: a
// bare "user:pw@host" is not a URL to the parsers whose errors this redacts.
// The class stays wide on purpose (commas and semicolons are legal in a
// userinfo): swallowing a bystander word is the safe failure, emitting a
// credential is not. One pattern for the gateway and Front Desk, which
// render the match differently (this keeps "***@", Front Desk drops the
// credential and the "@").
var URLUserinfoRE = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/?\s"]*@`)

// RedactURLUserinfo replaces the credential part of every URL found in text
// with "***@", for error strings and log lines that quote a URL a caller
// supplied. Text without a URL credential is returned unchanged.
func RedactURLUserinfo(text string) string {
	return URLUserinfoRE.ReplaceAllString(text, "${1}***@")
}

// URLParseReason strips the quoted URL from a url.Parse error and keeps the
// reason. The URL a caller supplied may carry a credential in its userinfo or
// query, and an error from parsing it reaches logs and API responses; the
// caller already knows which URL it sent. The reason itself quotes the bytes
// it refused (`invalid port ":sk-..." after host`, `ParseAddr("sk-...")`), and
// a credential pasted into the wrong place of a URL lands in exactly those, so
// every quoted span is masked too.
func URLParseReason(err error) error {
	ue, ok := errors.AsType[*url.Error](err)
	if !ok {
		return err
	}
	if reason := ue.Err.Error(); strings.Contains(reason, `"`) {
		return errors.New(quotedSpan.ReplaceAllString(reason, `"***"`))
	}
	return ue.Err
}

// quotedSpan is a double-quoted span of a url.Parse reason.
var quotedSpan = regexp.MustCompile(`"[^"]*"`)

// credentialParamWords are the parameter names a key may travel in (a custom
// gateway may authenticate by ?key=), split into words. Only these count:
// treating every query value as a secret would redact Azure's
// ?api-version=... out of the one diagnostic an operator needs when a version
// is refused. One list for both layers: the base_url validator folds a name
// and looks it up (credentialQueryParams), and the text masker matches the
// words joined by an optional "-" or "_" (secretParamShape), so a name the
// validator refuses is never one the masker lets through.
var credentialParamWords = [][]string{
	{"key"}, {"api", "key"}, {"x", "api", "key"}, {"x", "goog", "api", "key"},
	{"access", "key"}, {"secret", "key"}, {"client", "key"},
	{"token"}, {"api", "token"}, {"access", "token"}, {"auth", "token"},
	{"refresh", "token"}, {"client", "token"},
	{"secret"}, {"client", "secret"}, {"access", "secret"}, {"auth", "secret"}, {"refresh", "secret"},
	{"password"}, {"client", "password"}, {"access", "password"}, {"auth", "password"}, {"refresh", "password"},
	// Signed-URL credentials: Azure SAS and S3 presigned URLs.
	{"sig"}, {"signature"}, {"x", "amz", "signature"},
	{"x", "amz", "credential"}, {"x", "amz", "security", "token"},
}

// credentialQueryParams holds credentialParamWords in the form
// credentialParamName reduces a name to.
var credentialQueryParams = func() map[string]bool {
	m := make(map[string]bool, len(credentialParamWords))
	for _, words := range credentialParamWords {
		m[strings.Join(words, "")] = true
	}
	return m
}()

// credentialParamName folds the spellings of one name together: case, and the
// "-" or "_" between its words, so api_token, api-token and apiToken match.
var credentialParamName = strings.NewReplacer("-", "", "_", "")

// IsCredentialQueryParam reports whether a query parameter of that name
// carries a credential. name is the decoded name.
func IsCredentialQueryParam(name string) bool {
	return credentialQueryParams[credentialParamName.Replace(strings.ToLower(name))]
}
