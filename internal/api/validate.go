package api

import (
	"fmt"
	"strings"
	"unicode"
)

// validateStringLength checks that a string value is within [minLen, maxLen] after trimming.
func validateStringLength(field, value string, minLen, maxLen int) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < minLen {
		return fmt.Errorf("%s must be at least %d characters", field, minLen)
	}
	if len(trimmed) > maxLen {
		return fmt.Errorf("%s must be at most %d characters", field, maxLen)
	}
	return nil
}

// validateStringPtrLength checks that a non-nil *string is within [minLen, maxLen] after trimming.
// Returns nil if the pointer is nil.
func validateStringPtrLength(field string, value *string, minLen, maxLen int) error {
	if value == nil {
		return nil
	}
	return validateStringLength(field, *value, minLen, maxLen)
}

// validateIntRange checks that an int value is within [minVal, maxVal].
func validateIntRange(field string, value, minVal, maxVal int) error {
	if value < minVal {
		return fmt.Errorf("%s must be at least %d", field, minVal)
	}
	if value > maxVal {
		return fmt.Errorf("%s must be at most %d", field, maxVal)
	}
	return nil
}

// validateIntPtrRange checks that a non-nil *int is within [minVal, maxVal].
// Returns nil if the pointer is nil.
func validateIntPtrRange(field string, value *int, minVal, maxVal int) error {
	if value == nil {
		return nil
	}
	return validateIntRange(field, *value, minVal, maxVal)
}

// validateFloatRange checks that a float64 value is within [minVal, maxVal].
func validateFloatRange(field string, value, minVal, maxVal float64) error {
	if value < minVal {
		return fmt.Errorf("%s must be at least %g", field, minVal)
	}
	if value > maxVal {
		return fmt.Errorf("%s must be at most %g", field, maxVal)
	}
	return nil
}

// validateFloatPtrRange checks that a non-nil *float64 is within [minVal, maxVal].
// Returns nil if the pointer is nil.
func validateFloatPtrRange(field string, value *float64, minVal, maxVal float64) error {
	if value == nil {
		return nil
	}
	return validateFloatRange(field, *value, minVal, maxVal)
}

// validateMapSize checks that a map has at most maxEntries entries.
func validateMapSize(field string, m map[string]bool, maxEntries int) error {
	if len(m) > maxEntries {
		return fmt.Errorf("%s must have at most %d entries", field, maxEntries)
	}
	return nil
}

// validateNameString trims, checks length, and rejects control/invisible characters.
// Returns the trimmed value and an error if validation fails.
func validateNameString(field, value string, minLen, maxLen int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if err := validateStringLength(field, trimmed, minLen, maxLen); err != nil {
		return trimmed, err
	}
	if err := validatePrintable(field, trimmed); err != nil {
		return trimmed, err
	}
	return trimmed, nil
}

// validateNamePtr trims, checks length, and rejects control/invisible characters
// for a *string field. Returns the trimmed pointer and an error if validation fails.
// Returns (nil, nil) if the pointer is nil.
func validateNamePtr(field string, value *string, minLen, maxLen int) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed, err := validateNameString(field, *value, minLen, maxLen)
	if err != nil {
		return nil, err
	}
	return &trimmed, nil
}

// validatePrintable rejects strings containing control characters, invisible
// Unicode characters, and non-standard whitespace. Normal space (U+0020) is allowed.
func validatePrintable(field, value string) error {
	for _, r := range value {
		switch {
		case unicode.IsControl(r):
			return fmt.Errorf("%s contains invalid control characters", field)
		case r == '\u200B', r == '\u200C', r == '\u200D', // zero-width joiner/space
			r == '\uFEFF',                                              // BOM
			r == '\u2060',                                              // word joiner
			r == '\u2061', r == '\u2062', r == '\u2063', r == '\u2064': // invisible operators
			return fmt.Errorf("%s contains invisible characters", field)
		case r == '\u00A0', // non-breaking space
			r == '\u2028', r == '\u2029', // line/paragraph separator
			r == '\u3000': // ideographic space
			return fmt.Errorf("%s contains non-standard whitespace characters", field)
		}
	}
	return nil
}

// trimString returns a trimmed copy of the string.
func trimString(s string) string {
	return strings.TrimSpace(s)
}
