package util

import (
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
