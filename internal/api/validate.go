package api

import (
	"fmt"
	"strings"
	"unicode"
)

// validateStringLength checks that a string value is within [min, max] after trimming.
func validateStringLength(field, value string, min, max int) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < min {
		return fmt.Errorf("%s must be at least %d characters", field, min)
	}
	if len(trimmed) > max {
		return fmt.Errorf("%s must be at most %d characters", field, max)
	}
	return nil
}

// validateStringPtrLength checks that a non-nil *string is within [min, max] after trimming.
// Returns nil if the pointer is nil.
func validateStringPtrLength(field string, value *string, min, max int) error {
	if value == nil {
		return nil
	}
	return validateStringLength(field, *value, min, max)
}

// validateIntRange checks that an int value is within [min, max].
func validateIntRange(field string, value, min, max int) error {
	if value < min {
		return fmt.Errorf("%s must be at least %d", field, min)
	}
	if value > max {
		return fmt.Errorf("%s must be at most %d", field, max)
	}
	return nil
}

// validateIntPtrRange checks that a non-nil *int is within [min, max].
// Returns nil if the pointer is nil.
func validateIntPtrRange(field string, value *int, min, max int) error {
	if value == nil {
		return nil
	}
	return validateIntRange(field, *value, min, max)
}

// validateFloatRange checks that a float64 value is within [min, max].
func validateFloatRange(field string, value, min, max float64) error {
	if value < min {
		return fmt.Errorf("%s must be at least %g", field, min)
	}
	if value > max {
		return fmt.Errorf("%s must be at most %g", field, max)
	}
	return nil
}

// validateFloatPtrRange checks that a non-nil *float64 is within [min, max].
// Returns nil if the pointer is nil.
func validateFloatPtrRange(field string, value *float64, min, max float64) error {
	if value == nil {
		return nil
	}
	return validateFloatRange(field, *value, min, max)
}

// validateMapSize checks that a map has at most max entries.
func validateMapSize(field string, m map[string]bool, max int) error {
	if len(m) > max {
		return fmt.Errorf("%s must have at most %d entries", field, max)
	}
	return nil
}


// validateNameString trims, checks length, and rejects control/invisible characters.
// Returns the trimmed value and an error if validation fails.
func validateNameString(field, value string, min, max int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if err := validateStringLength(field, trimmed, min, max); err != nil {
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
func validateNamePtr(field string, value *string, min, max int) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed, err := validateNameString(field, *value, min, max)
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
			r == '\uFEFF', // BOM
			r == '\u2060', // word joiner
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

