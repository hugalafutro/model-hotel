package util

import "strings"

func SanitizeBaseURL(raw string) string {
	return strings.TrimSuffix(raw, "/")
}