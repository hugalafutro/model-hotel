package util

import "strings"

func SanitizeBaseURL(raw string) string {
	return strings.TrimSuffix(raw, "/")
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
