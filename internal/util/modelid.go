package util

import "strings"

// ModelIDSegments splits a model ID into its name segments: lower-cased and
// cut on the separators HuggingFace-style ids and vendor prefixes use
// (org/name-with_parts:tag, vendor.name). A family or endpoint token is then
// matched as a whole segment or a segment prefix, so a name that merely
// contains the letters does not match.
func ModelIDSegments(modelID string) []string {
	return strings.FieldsFunc(strings.ToLower(modelID), func(r rune) bool {
		switch r {
		case '/', '-', '_', '.', ':', ' ':
			return true
		}
		return false
	})
}
