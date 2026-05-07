package util

import "strings"

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
